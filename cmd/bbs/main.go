// Command bbs runs the NullModem BBS telnet and SSH front-ends.
package main

import (
	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/guard"
	// Time zones built in: TZ (e.g. Europe/Zurich) works whether the
	// image has a zoneinfo database or not.
	_ "time/tzdata"

	"context"
	"errors"
	"flag"
	"git.maik.ch/nullmodem/bbs/internal/maintenance"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/applog"
	"git.maik.ch/nullmodem/bbs/internal/bbs"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/doors"
	"git.maik.ch/nullmodem/bbs/internal/file"
	"git.maik.ch/nullmodem/bbs/internal/hostkey"
	"git.maik.ch/nullmodem/bbs/internal/menu"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/services"
	"git.maik.ch/nullmodem/bbs/internal/session"
	"git.maik.ch/nullmodem/bbs/internal/ssh"
	"git.maik.ch/nullmodem/bbs/internal/telnet"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/bbs/internal/version"
	"git.maik.ch/nullmodem/kit/ansi"
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

	menus, err := menu.LoadDir(cfg.BBS.MenusDir)
	if err != nil {
		logger.Fatal("loading menus: %v", err)
	}

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
		Chat:             chat.NewStore(sqlDB),
		Security:         security,
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
	if inst, err := serviceStore.Register(services.BBS, version.Short()); err != nil {
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

	errCh := make(chan error, 2)

	if cfg.Telnet.Enabled {
		telnetSrv := &telnet.Server{
			Addr:    cfg.Telnet.Addr,
			Handler: func(s *telnet.Session) { srv.Handle(s) },
		}
		go func() {
			logger.Info("telnet server listening on %s", cfg.Telnet.Addr)
			errCh <- telnetSrv.ListenAndServe()
		}()
	}

	if cfg.SSH.Enabled {
		signer, err := hostkey.LoadOrCreate(cfg.SSH.HostKeyPath)
		if err != nil {
			logger.Fatal("ssh host key: %v", err)
		}
		sshSrv := &ssh.Server{
			Addr:    cfg.SSH.Addr,
			HostKey: signer,
			Handler: func(s *ssh.Session) { srv.Handle(s) },
		}
		go func() {
			logger.Info("ssh server listening on %s", cfg.SSH.Addr)
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
			ANSI16:            d.ANSI16,
			Remote:            d.Remote,
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
