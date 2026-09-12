// Command bbs runs the NullModem BBS telnet and SSH front-ends.
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/bbs"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/hostkey"
	"git.maik.ch/swissmaik/nullmodem/internal/menu"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/ssh"
	"git.maik.ch/swissmaik/nullmodem/internal/telnet"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
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
	users := user.NewStore(sqlDB)
	messages := message.NewStore(sqlDB)

	menus, err := menu.LoadDir(cfg.BBS.MenusDir)
	if err != nil {
		log.Fatalf("loading menus: %v", err)
	}

	welcomeScreen, err := ansi.LoadScreen(filepath.Join(cfg.BBS.ScreensDir, "welcome.ans"))
	if err != nil {
		log.Fatalf("loading welcome screen: %v", err)
	}

	srv := bbs.NewServer(bbs.Options{
		BBSName:       cfg.BBS.Name,
		SysopName:     cfg.BBS.Sysop,
		Users:         users,
		Menus:         menus,
		Messages:      messages,
		NewUserSL:     cfg.BBS.NewUserSL,
		WelcomeScreen: welcomeScreen,
	})

	errCh := make(chan error, 2)

	if cfg.Telnet.Enabled {
		telnetSrv := &telnet.Server{
			Addr:    cfg.Telnet.Addr,
			Handler: func(s *telnet.Session) { srv.Handle(s) },
		}
		go func() {
			log.Printf("telnet server listening on %s", cfg.Telnet.Addr)
			errCh <- telnetSrv.ListenAndServe()
		}()
	}

	if cfg.SSH.Enabled {
		signer, err := hostkey.LoadOrCreate(cfg.SSH.HostKeyPath)
		if err != nil {
			log.Fatalf("ssh host key: %v", err)
		}
		sshSrv := &ssh.Server{
			Addr:    cfg.SSH.Addr,
			HostKey: signer,
			Handler: func(s *ssh.Session) { srv.Handle(s) },
		}
		go func() {
			log.Printf("ssh server listening on %s", cfg.SSH.Addr)
			errCh <- sshSrv.ListenAndServe()
		}()
	}

	if !cfg.Telnet.Enabled && !cfg.SSH.Enabled {
		log.Fatal("both telnet and ssh are disabled in config; nothing to serve")
	}

	log.Fatal(<-errCh)
}
