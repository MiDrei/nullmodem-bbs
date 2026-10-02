// Command web runs the NullModem BBS admin REST API and serves the
// built SvelteKit admin UI.
package main

import (
	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/community"
	"git.maik.ch/nullmodem/bbs/internal/guard"
	"git.maik.ch/nullmodem/bbs/internal/health"
	"git.maik.ch/nullmodem/bbs/internal/nodelist"
	"net"
	// Time zones built in: TZ (e.g. Europe/Zurich) works whether the
	// image has a zoneinfo database or not.
	_ "time/tzdata"

	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/backup"
	"git.maik.ch/nullmodem/bbs/internal/maintenance"
	"git.maik.ch/nullmodem/bbs/internal/push"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/applog"
	"git.maik.ch/nullmodem/bbs/internal/archive"
	"git.maik.ch/nullmodem/bbs/internal/areafix"
	"git.maik.ch/nullmodem/bbs/internal/binkplog"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/file"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/services"
	"git.maik.ch/nullmodem/bbs/internal/session"
	"git.maik.ch/nullmodem/bbs/internal/stats"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/bbs/internal/version"
	"git.maik.ch/nullmodem/bbs/internal/web"
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
	maintenance.ApplyLimits(bbsCfg.Maintenance)

	if err := migrateNetworks(cfg.BBSConfigPath, filepath.Dir(cfg.DatabasePath), bbsCfg, sqlDB, logger); err != nil {
		log.Fatalf("migrating networks: %v", err)
	}

	// A restart asked for in the web admin (see internal/services) --
	// the request that asked for it has been answered by the time Run
	// sees it.
	if inst, err := services.NewStore(sqlDB).Register(services.Web, version.Short()); err != nil {
		logger.Warn("registering with the service list: %v", err)
	} else {
		go inst.Run(context.Background(), func(string) {
			logger.Info("web daemon restarting, as asked in the web admin")
			os.Exit(0)
		})
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
		Services:      services.NewStore(sqlDB),
		Logger:        logger,
		EchoAreafix:   areafix.NewEchoStore(sqlDB),
		FileAreafix:   areafix.NewFileStore(sqlDB),
		Archive:       archive.NewStore(sqlDB, filepath.Join(filepath.Dir(cfg.DatabasePath), "inbound-archive")),
		BinkpLog:      binkplog.NewStore(sqlDB, filepath.Join(filepath.Dir(cfg.DatabasePath), "binkp-sessions")),
		BBSConfigPath: cfg.BBSConfigPath,
		WebConfigPath: *configPath,
		FTNAddress:    bbsCfg.PrimaryFTNAddress(),
		JWTSecret:     secret,
		StaticDir:     cfg.StaticDir,
		DB:            sqlDB,
		DBPath:        cfg.DatabasePath,
		Chat:          chat.NewStore(sqlDB),
		Nodelist:      nodelist.NewStore(sqlDB),
		Community:     community.NewStore(sqlDB),
		Stats:         stats.NewStore(sqlDB),
		TerminalAddr:  terminalAddr(cfg, bbsCfg),
	}

	// Login protection, shared with the bbs daemon through the database;
	// its settings re-read every half minute.
	current := config.Cached(cfg.BBSConfigPath, 30*time.Second, bbsCfg)
	srv.Guard = guard.New(sqlDB, func() guard.Settings {
		on, max, window, lockout, maxLockout := current().Security.GuardSettings()
		return guard.Settings{Enabled: on, MaxFailures: max, Window: window, Lockout: lockout, MaxLockout: maxLockout}
	}, logger)

	// The mobile reader's notifications: the key pair beside the JWT
	// secret, and the notifier looking for new mail.
	if keys, err := push.LoadOrCreateKeys(filepath.Join(filepath.Dir(cfg.JWTSecretPath), "vapid.json")); err != nil {
		logger.Warn("notifications off: %v", err)
	} else {
		srv.Push = push.NewSender(keys, push.NewStore(sqlDB))
		go (&push.Notifier{DB: sqlDB, Sender: srv.Push, Logger: logger}).Run(context.Background())
	}

	// The nightly backup, by the settings in bbs.yaml as they are each
	// time.
	go backup.Nightly(context.Background(), func() (bool, int, backup.Options, backup.Sources) {
		c, err := config.Load(cfg.BBSConfigPath)
		if err != nil {
			c = bbsCfg
		}
		return c.Backup.On(), c.Backup.RunHour(), web.BackupOptions(c), srv.BackupSources(c)
	}, logger)

	// Watching that everything keeps working: problems go to the
	// sysops' phones (when they have the reader's notifications on)
	// and onto the dashboard.
	go health.Monitor(context.Background(), health.Env{
		DB:        sqlDB,
		Config:    current,
		Self:      services.Web,
		StartedAt: time.Now(),
		Disk:      web.DiskUsage,
	}, 5*time.Minute, logger, func(p health.Problem, ok bool) {
		if srv.Push == nil {
			return
		}
		n := push.Notification{Title: "⚠ " + p.Title, Body: p.Detail, URL: "/admin/dashboard", Tag: "health-" + p.Key}
		if ok {
			n = push.Notification{Title: "✓ Fixed: " + p.Title, URL: "/admin/dashboard", Tag: "health-" + p.Key}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Push.ToSysops(ctx, sqlDB, n); err != nil {
			logger.Warn("health notification: %v", err)
		}
	})

	logger.Info("web admin API listening on %s", cfg.Addr)
	logger.Fatal("%v", http.ListenAndServe(cfg.Addr, srv.Routes()))
}

// migrateNetworks finishes what config.Load started for a config
// written before networks had short names (see config.Config.
// NetworkRenames): it renames the area groups in the database and, as
// the one daemon that writes bbs.yaml, saves the config with its new
// networks section -- keeping a copy of the old file in dataDir first
// (not next to bbs.yaml: in Docker only the file itself is mounted,
// its directory isn't writable).
func migrateNetworks(path, dataDir string, c *config.Config, sqlDB *sql.DB, logger *applog.Logger) error {
	if len(c.NetworkRenames) == 0 {
		return nil
	}
	n, err := c.ApplyNetworkRenames(message.NewStore(sqlDB), file.NewStore(sqlDB, c.BBS.FilesDir))
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s for its backup: %w", path, err)
	}
	backup := filepath.Join(dataDir, filepath.Base(path)+".bak-networks-"+time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backup, old, 0o644); err != nil {
		return fmt.Errorf("backing up %s: %w", path, err)
	}
	if err := config.Save(path, c); err != nil {
		return err
	}
	names := make([]string, 0, len(c.Networks))
	for _, nw := range c.Networks {
		names = append(names, nw.Name+"@"+nw.Domain)
	}
	logger.Info("networks set up from the uplinks (%s), %d area group(s) renamed; previous config kept as %s",
		strings.Join(names, ", "), n, filepath.Base(backup))
	return nil
}

// terminalAddr is where the web terminal reaches the BBS's Telnet port:
// as configured, else this machine at bbs.yaml's Telnet port; "" when
// Telnet is off.
func terminalAddr(cfg *config.WebConfig, bbsCfg *config.Config) string {
	if cfg.TerminalAddr != "" {
		return cfg.TerminalAddr
	}
	if !bbsCfg.Telnet.Enabled {
		return ""
	}
	_, port, err := net.SplitHostPort(bbsCfg.Telnet.Addr)
	if err != nil {
		return ""
	}
	return net.JoinHostPort("127.0.0.1", port)
}
