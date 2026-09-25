// Command bbs runs the NullModem BBS telnet and SSH front-ends.
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"

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
	"git.maik.ch/nullmodem/bbs/internal/session"
	"git.maik.ch/nullmodem/bbs/internal/ssh"
	"git.maik.ch/nullmodem/bbs/internal/telnet"
	"git.maik.ch/nullmodem/bbs/internal/user"
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
	logger := applog.NewLogger(logs, "bbs")

	users := user.NewStore(sqlDB)
	messages := message.NewStore(sqlDB)
	files := file.NewStore(sqlDB, cfg.BBS.FilesDir)
	netmailStore := netmail.NewStore(sqlDB)

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

	var doorList []doors.Door
	for _, d := range cfg.Doors {
		doorList = append(doorList, doors.Door{
			Name:            d.Name,
			Kind:            d.Kind,
			MinSL:           d.MinSL,
			Exe:             d.Exe,
			Dir:             d.Dir,
			Args:            d.Args,
			DOSBoxDir:       d.DOSBoxDir,
			DOSBoxLaunchCmd: d.DOSBoxLaunchCmd,
		})
	}

	srv := bbs.NewServer(bbs.Options{
		BBSName:       cfg.BBS.Name,
		SysopName:     cfg.BBS.Sysop,
		FTNAddress:    cfg.PrimaryFTNAddress(),
		Users:         users,
		Menus:         menus,
		Messages:      messages,
		Files:         files,
		Netmail:       netmailStore,
		Doors:         doorList,
		Nodes:         nodes,
		NewUserSL:     cfg.BBS.NewUserSL,
		WelcomeScreen: welcomeScreen,
		ScreensDir:    cfg.BBS.ScreensDir,
		Logger:        logger,
	})

	// File and QWK transfers over Telnet/SSH run Synchronet's sexyz.
	// Without it every transfer fails the moment it starts, so say so
	// once, loudly, where the sysop looks -- not only per attempt.
	if _, err := exec.LookPath("sexyz"); err != nil {
		logger.Warn("sexyz not found on PATH: Zmodem file and QWK transfers over Telnet/SSH will fail (see docs/building-sexyz.md)")
	}

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
