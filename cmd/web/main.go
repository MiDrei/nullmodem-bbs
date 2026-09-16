// Command web runs the NullModem BBS admin REST API and serves the
// built SvelteKit admin UI.
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"

	"git.maik.ch/swissmaik/nullmodem/internal/applog"
	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/session"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
	"git.maik.ch/swissmaik/nullmodem/internal/web"
)

func main() {
	configPath := flag.String("config", "configs/web.yaml", "path to web daemon config file")
	flag.Parse()

	cfg, err := config.LoadWeb(*configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Fatalf("loading config: %v", err)
		}
		log.Printf("no config at %s, using defaults", *configPath)
		cfg = config.DefaultWeb()
	}

	sqlDB, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer sqlDB.Close()

	logs := applog.NewStore(sqlDB)
	logger := applog.NewLogger(logs, "web")

	secret, err := web.LoadOrCreateJWTSecret(cfg.JWTSecretPath)
	if err != nil {
		log.Fatalf("jwt secret: %v", err)
	}

	bbsCfg, err := config.Load(cfg.BBSConfigPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Fatalf("loading bbs config: %v", err)
		}
		log.Printf("no bbs config at %s, using defaults", cfg.BBSConfigPath)
		bbsCfg = config.Default()
	}

	srv := &web.Server{
		Users:    user.NewStore(sqlDB),
		Messages: message.NewStore(sqlDB),
		Files:    file.NewStore(sqlDB, bbsCfg.BBS.FilesDir),
		Netmail:  netmail.NewStore(sqlDB),
		// No ClearAll: the web daemon must never wipe the BBS daemon's
		// live session state just by starting or restarting.
		Nodes:         session.NewStore(sqlDB),
		Logs:          logs,
		Logger:        logger,
		EchoAreafix:   areafix.NewEchoStore(sqlDB),
		FileAreafix:   areafix.NewFileStore(sqlDB),
		BBSConfigPath: cfg.BBSConfigPath,
		JWTSecret:     secret,
		StaticDir:     cfg.StaticDir,
	}

	logger.Info("web admin API listening on %s", cfg.Addr)
	logger.Fatal("%v", http.ListenAndServe(cfg.Addr, srv.Routes()))
}
