// Command bbs runs the NullModem BBS telnet and SSH front-ends.
package main

import (
	"github.com/midrei/nullmodem-bbs/internal/chat"
	"github.com/midrei/nullmodem-bbs/internal/community"
	"github.com/midrei/nullmodem-bbs/internal/guard"
	"github.com/midrei/nullmodem-bbs/internal/i18n"
	"github.com/midrei/nullmodem-bbs/internal/nodelist"
	// Time zones built in: TZ (e.g. Europe/Zurich) works whether the
	// image has a zoneinfo database or not.
	_ "time/tzdata"

	"context"
	"errors"
	"flag"
	"github.com/midrei/nullmodem-bbs/internal/maintenance"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/applog"
	"github.com/midrei/nullmodem-bbs/internal/bbs"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/doors"
	"github.com/midrei/nullmodem-bbs/internal/emailgw"
	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-bbs/internal/hostkey"
	"github.com/midrei/nullmodem-bbs/internal/menu"
	"github.com/midrei/nullmodem-bbs/internal/message"
	"github.com/midrei/nullmodem-bbs/internal/netmail"
	"github.com/midrei/nullmodem-bbs/internal/services"
	"github.com/midrei/nullmodem-bbs/internal/session"
	"github.com/midrei/nullmodem-bbs/internal/ssh"
	"github.com/midrei/nullmodem-bbs/internal/stats"
	"github.com/midrei/nullmodem-bbs/internal/telnet"
	"github.com/midrei/nullmodem-bbs/internal/user"
	"github.com/midrei/nullmodem-bbs/internal/version"
	"github.com/midrei/nullmodem-kit/ansi"
)

func main() {
	configPath := flag.String("config", "configs/bbs.yaml", "path to BBS config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Fatalf("loading config: %v", err)
		}
		log.Printf("no config at %s, using defaults", *configPath)
		cfg = config.Default()
	}

	sqlDB, err := db.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer sqlDB.Close()

	logs := applog.NewStore(sqlDB)
	maintenance.ApplyLimits(cfg.Maintenance)
	logger := applog.NewLogger(logs, "bbs")

	users := user.NewStore(sqlDB)
	messages := message.NewStore(sqlDB)
	files := file.NewStore(sqlDB, cfg.BBS.FilesDir)
	netmailStore := netmail.NewStore(sqlDB)
	if n, err := cfg.ApplyNetworkRenames(messages, files); err != nil {
		logger.Fatal("renaming area groups to network names: %v", err)
	} else if n > 0 {
		logger.Info("renamed %d area group(s) to their network's short name", n)
	}

	nodes := session.NewStore(sqlDB)
	if err := nodes.ClearAll(); err != nil {
		logger.Fatal("initializing session tracking: %v", err)
	}

	// The texts in every language, with the sysop's changes (the web
	// admin's language editor; read again when they change).
	i18n.Use(i18n.New(cfg.TextsDir()))

	// Read again when a file changes (the web admin's menu editor).
	menus, err := menu.NewWatcher(cfg.BBS.MenusDir)
	if err != nil {
		logger.Fatal("loading menus: %v", err)
	}
	menus.OnError = func(err error) { logger.Warn("menus not reloaded, keeping the previous ones: %v", err) }

	welcomeScreen, err := ansi.LoadScreen(filepath.Join(cfg.BBS.ScreensDir, "welcome.ans"))
	if err != nil {
		logger.Fatal("loading welcome screen: %v", err)
	}

	// The door list is re-read from the config file each time a caller
	// opens the doors menu, so doors added or changed in the web admin
	// work without restarting this daemon. The startup list stands in
	// if the file can't be read at that moment.
	startupDoors := doorsFromConfig(cfg.Doors)
	loadDoors := func() []doors.Door {
		c, err := config.Load(*configPath)
		if err != nil {
			logger.Warn("reloading doors from %s: %v", *configPath, err)
			return startupDoors
		}
		return doorsFromConfig(c.Doors)
	}

	// Login protection and new-user approval, as set in the web admin
	// (re-read every half minute).
	current := config.Cached(*configPath, 30*time.Second, cfg)
	chatStore := chat.NewStore(sqlDB)
	chatStore.Quiet = chat.QuietSysops(users, func() bool { return current().BBS.ChatAnnounceSysops })
	security := func() config.SecurityConfig { return current().Security }
	loginGuard := guard.New(sqlDB, func() guard.Settings {
		on, max, window, lockout, maxLockout := security().GuardSettings()
		return guard.Settings{Enabled: on, MaxFailures: max, Window: window, Lockout: lockout, MaxLockout: maxLockout}
	}, logger)

	srv := bbs.NewServer(bbs.Options{
		BBSName:          cfg.BBS.Name,
		SysopName:        cfg.BBS.Sysop,
		FTNAddress:       cfg.PrimaryFTNAddress(),
		Users:            users,
		Menus:            menus,
		Messages:         messages,
		Files:            files,
		Netmail:          netmailStore,
		Doors:            startupDoors,
		LoadDoors:        loadDoors,
		Nodes:            nodes,
		NewUserSL:        cfg.BBS.NewUserSL,
		WelcomeScreen:    welcomeScreen,
		ScreensDir:       cfg.BBS.ScreensDir,
		Logger:           logger,
		LastCallers:      cfg.InterBBS.LastCallers,
		Guard:            loginGuard,
		FullScreenEditor: true,
		Chat:             chatStore,
		Nodelist:         nodelist.NewStore(sqlDB),
		Community:        community.NewStore(sqlDB),
		Stats:            stats.NewStore(sqlDB),
		Security:         security,
		Language:         func() string { return current().BBS.Language },
		Email:            func() config.EmailConfig { return current().Email },
		Forwards:         emailgw.NewForwards(sqlDB),
		SecurityLevels:   func(name func(string) string) []config.SecurityLevel { return current().Levels(name) },
	})

	// File and QWK transfers over Telnet/SSH run Synchronet's sexyz.
	// Without it every transfer fails the moment it starts, so say so
	// once, loudly, where the sysop looks -- not only per attempt.
	if _, err := exec.LookPath("sexyz"); err != nil {
		logger.Warn("sexyz not found on PATH: Zmodem file and QWK transfers over Telnet/SSH will fail (see docs/building-sexyz.md)")
	}

	// A restart asked for in the web admin (see internal/services):
	// "idle" waits until no caller is online, "now" doesn't.
	serviceStore := services.NewStore(sqlDB)
	if inst, err := serviceStore.Register(services.BBS, version.ShortBuild()); err != nil {
		logger.Warn("registering with the service list: %v", err)
	} else {
		go inst.Run(context.Background(), func(mode string) {
			if mode == services.ModeIdle {
				for {
					online, err := nodes.List()
					if err == nil && len(online) == 0 {
						break
					}
					time.Sleep(2 * time.Second)
				}
			}
			logger.Info("bbs daemon restarting, as asked in the web admin (%s)", mode)
			os.Exit(0)
		})
	}

	// Doors' background programs (uMRC's umrc-bridge) run as long as
	// their door is set up, re-read from the config like the door list.
	supervisor := &doors.Supervisor{
		Programs: func() []doors.Program {
			c, err := config.Load(*configPath)
			if err != nil {
				logger.Warn("reloading doors from %s: %v", *configPath, err)
				return programsFromConfig(cfg.Doors)
			}
			return programsFromConfig(c.Doors)
		},
		Store:  serviceStore,
		Logger: logger,
	}
	go supervisor.Run(context.Background())

	// Doors' daily maintenance (new turns, the day's events), headless,
	// at each door's time -- or when "Run now" is pressed in the admin.
	go (&doors.Scheduler{DB: sqlDB, Doors: loadDoors, Logger: logger}).Run(context.Background())

	errCh := make(chan error, 2)

	if cfg.Telnet.Enabled {
		telnetSrv := &telnet.Server{
			Addr:      cfg.Telnet.Addr,
			ProxyAddr: cfg.Telnet.ProxyAddr,
			Handler:   func(s *telnet.Session) { srv.Handle(s) },
		}
		go func() {
			logger.Info("telnet server listening on %s", cfg.Telnet.Addr)
			if cfg.Telnet.ProxyAddr != "" {
				logger.Info("telnet server listening for a proxy (PROXY protocol) on %s", cfg.Telnet.ProxyAddr)
			}
			errCh <- telnetSrv.ListenAndServe()
		}()
	}

	if cfg.SSH.Enabled {
		signer, err := hostkey.LoadOrCreate(cfg.SSH.HostKeyPath)
		if err != nil {
			logger.Fatal("ssh host key: %v", err)
		}
		sshSrv := &ssh.Server{
			Addr:      cfg.SSH.Addr,
			ProxyAddr: cfg.SSH.ProxyAddr,
			HostKey:   signer,
			Handler:   func(s *ssh.Session) { srv.Handle(s) },
		}
		go func() {
			logger.Info("ssh server listening on %s", cfg.SSH.Addr)
			if cfg.SSH.ProxyAddr != "" {
				logger.Info("ssh server listening for a proxy (PROXY protocol) on %s", cfg.SSH.ProxyAddr)
			}
			errCh <- sshSrv.ListenAndServe()
		}()
	}

	if !cfg.Telnet.Enabled && !cfg.SSH.Enabled {
		logger.Fatal("both telnet and ssh are disabled in config; nothing to serve")
	}

	logger.Fatal("%v", <-errCh)
}

// doorsFromConfig maps the config's door entries onto internal/doors'
// own type.
func doorsFromConfig(entries []config.DoorConfig) []doors.Door {
	var list []doors.Door
	for _, d := range entries {
		list = append(list, doors.Door{
			Name:              d.Name,
			Kind:              d.Kind,
			MinSL:             d.MinSL,
			Exe:               d.Exe,
			Dir:               d.Dir,
			Args:              d.Args,
			DOSBoxDir:         d.DOSBoxDir,
			DOSBoxLaunchCmd:   d.DOSBoxLaunchCmd,
			DropFile:          d.DropFile,
			DropFileInDoorDir: d.DropFileInDoorDir,
			LockFiles:         d.LockFiles,
			Stdio:             d.Stdio,
			Console:           d.Console,
			ANSI16:            d.ANSI16,
			Remote:            d.Remote,
			Daily:             d.Daily,
			DailyAt:           d.DailyAt,
			Bulletins:         bulletinsFromConfig(d.Bulletins),
		})
	}
	return list
}

// programsFromConfig lists the background programs of the native
// doors that have one and are installed.
func programsFromConfig(entries []config.DoorConfig) []doors.Program {
	var list []doors.Program
	for _, d := range entries {
		if len(d.Program) == 0 || d.Kind != "" || d.Dir == "" {
			continue
		}
		if _, err := os.Stat(d.Dir); err != nil {
			continue
		}
		list = append(list, doors.Program{Door: d.Name, Dir: d.Dir, Command: d.Program})
	}
	return list
}

func bulletinsFromConfig(in []config.DoorBulletin) []doors.Bulletin {
	var out []doors.Bulletin
	for _, b := range in {
		out = append(out, doors.Bulletin{Title: b.Title, File: b.File, Public: b.Public})
	}
	return out
}
