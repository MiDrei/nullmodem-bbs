// Command web runs the NullModem BBS admin REST API and serves the
// built SvelteKit admin UI.
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"

	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
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

	secret, err := web.LoadOrCreateJWTSecret(cfg.JWTSecretPath)
	if err != nil {
		log.Fatalf("jwt secret: %v", err)
	}

	srv := &web.Server{
		Users:         user.NewStore(sqlDB),
		BBSConfigPath: cfg.BBSConfigPath,
		JWTSecret:     secret,
		StaticDir:     cfg.StaticDir,
	}

	log.Printf("web admin API listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Routes()))
}
