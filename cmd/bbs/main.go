// Command bbs runs the NullModem BBS telnet and SSH front-ends.
package main

import (
	"errors"
	"flag"
	"log"
	"os"

	"git.maik.ch/swissmaik/nullmodem/internal/bbs"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/hostkey"
	"git.maik.ch/swissmaik/nullmodem/internal/ssh"
	"git.maik.ch/swissmaik/nullmodem/internal/telnet"
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

	srv := bbs.NewServer(cfg.BBS.Name, cfg.BBS.Sysop)

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
