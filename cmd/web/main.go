// Command web runs the NullModem BBS admin REST API and serves the
// built SvelteKit admin UI.
package main

import (
	"github.com/midrei/nullmodem-bbs/internal/chat"
	"github.com/midrei/nullmodem-bbs/internal/community"
	"github.com/midrei/nullmodem-bbs/internal/doors"
	"github.com/midrei/nullmodem-bbs/internal/emailgw"
	"github.com/midrei/nullmodem-bbs/internal/guard"
	"github.com/midrei/nullmodem-bbs/internal/health"
	"github.com/midrei/nullmodem-bbs/internal/i18n"
	"github.com/midrei/nullmodem-bbs/internal/nodelist"
	"net"
	// Time zones built in: TZ (e.g. Europe/Zurich) works whether the
	// image has a zoneinfo database or not.
	_ "time/tzdata"

	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"github.com/midrei/nullmodem-bbs/internal/backup"
	"github.com/midrei/nullmodem-bbs/internal/maintenance"
	"github.com/midrei/nullmodem-bbs/internal/push"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/applog"
	"github.com/midrei/nullmodem-bbs/internal/archive"
	"github.com/midrei/nullmodem-bbs/internal/areafix"
	"github.com/midrei/nullmodem-bbs/internal/binkplog"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/discord"
	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-bbs/internal/matrix"
	"github.com/midrei/nullmodem-bbs/internal/message"
	"github.com/midrei/nullmodem-bbs/internal/netmail"
	"github.com/midrei/nullmodem-bbs/internal/offsite"
	"github.com/midrei/nullmodem-bbs/internal/services"
	"github.com/midrei/nullmodem-bbs/internal/session"
	"github.com/midrei/nullmodem-bbs/internal/stats"
	"github.com/midrei/nullmodem-bbs/internal/user"
	"github.com/midrei/nullmodem-bbs/internal/version"
	"github.com/midrei/nullmodem-bbs/internal/web"
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
	// The texts in every language; the language editor writes the
	// sysop's changes there.
	i18n.Use(i18n.New(bbsCfg.TextsDir()))

	if err := migrateNetworks(cfg.BBSConfigPath, filepath.Dir(cfg.DatabasePath), bbsCfg, sqlDB, logger); err != nil {
		log.Fatalf("migrating networks: %v", err)
	}
	if err := migrateDoorConsoles(cfg.BBSConfigPath, filepath.Dir(cfg.DatabasePath), bbsCfg, logger); err != nil {
		logger.Warn("moving doors to a console: %v", err)
	}

	// A restart asked for in the web admin (see internal/services) --
	// the request that asked for it has been answered by the time Run
	// sees it.
	if inst, err := services.NewStore(sqlDB).Register(services.Web, version.ShortBuild()); err != nil {
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
	srv.Chat.Quiet = chat.QuietSysops(srv.Users, func() bool { return current().BBS.ChatAnnounceSysops })
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
		go (&push.Notifier{DB: sqlDB, Sender: srv.Push, Logger: logger, Lang: func(id int64) string {
			if u, err := srv.Users.ByID(id); err == nil && i18n.Valid(u.Language) {
				return u.Language
			}
			if c, err := config.Load(cfg.BBSConfigPath); err == nil && i18n.Valid(c.BBS.Language) {
				return c.BBS.Language
			}
			return i18n.Fallback
		}}).Run(context.Background())
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

	// The image's stock menus, for the menu editor's "new in this
	// version" (docker-entrypoint.sh seeds configs/ from them).
	if st, err := os.Stat("configs-defaults/menus"); err == nil && st.IsDir() {
		srv.MenuDefaultsDir = "configs-defaults/menus"
	}

	// Encrypted copies of the backups to a storage service.
	go (&offsite.Copier{DB: sqlDB, Logger: logger, Config: func() config.BackupConfig {
		c, err := config.Load(cfg.BBSConfigPath)
		if err != nil {
			c = current()
		}
		return c.Backup
	}}).Run(context.Background())

	// The monthly recap netmail to the sysops.
	go srv.RunRecaps(context.Background())

	// Whether the boards on the BBS list answer.
	if srv.Community != nil {
		go srv.Community.RunChecks(context.Background())
	}

	// The chat rooms' bridge to Discord, as set in the web admin.
	srv.Discord = &discord.Bridge{
		Chat:   srv.Chat,
		Logger: logger,
		// Read fresh: a token saved in the admin applies at once (Reload).
		Settings: func() discord.Settings {
			c, err := config.Load(cfg.BBSConfigPath)
			if err != nil {
				c = current()
			}
			return discord.Settings{DiscordConfig: c.Discord, BBSName: c.BBS.Name}
		},
	}
	go srv.Discord.Run(context.Background())
	srv.Matrix = &matrix.Bridge{
		Chat:   srv.Chat,
		Logger: logger,
		Settings: func() matrix.Settings {
			c, err := config.Load(cfg.BBSConfigPath)
			if err != nil {
				c = current()
			}
			return matrix.Settings{MatrixConfig: c.Matrix, BBSName: c.BBS.Name}
		},
	}
	go srv.Matrix.Run(context.Background())

	// The netmail <-> email gateway, as set in the web admin.
	srv.EmailGateway = &emailgw.Gateway{
		DB:      sqlDB,
		Netmail: srv.Netmail,
		Users:   srv.Users,
		Logger:  logger,
		Config: func() config.EmailConfig {
			c, err := config.Load(cfg.BBSConfigPath)
			if err != nil {
				c = current()
			}
			return c.Email
		},
		Lang: func(u *user.User) string {
			if i18n.Valid(u.Language) {
				return u.Language
			}
			if c, err := config.Load(cfg.BBSConfigPath); err == nil && i18n.Valid(c.BBS.Language) {
				return c.BBS.Language
			}
			return i18n.Fallback
		},
		BBSName: func() string { return current().BBS.Name },
	}
	go srv.EmailGateway.Run(context.Background())
	go srv.RunDoorUpdateCheck(context.Background())

	// Watching that everything keeps working: problems go to the
	// sysops' phones (when they have the reader's notifications on)
	// and onto the dashboard.
	go health.Monitor(context.Background(), health.Env{
		DB:        sqlDB,
		Config:    current,
		Self:      services.Web,
		StartedAt: time.Now(),
		Disk:      web.DiskUsage,
		Extra: func() []health.Problem {
			var out []health.Problem
			if down, detail := srv.Discord.Down(15 * time.Minute); down {
				out = append(out, health.Problem{Key: "discord", Title: i18n.Ref("health.discord_down"), Detail: detail})
			}
			if down, detail := srv.Matrix.Down(15 * time.Minute); down {
				out = append(out, health.Problem{Key: "matrix", Title: i18n.Ref("health.matrix_down"), Detail: detail})
			}
			if down, detail := srv.EmailGateway.Down(30 * time.Minute); down {
				out = append(out, health.Problem{Key: "email", Title: i18n.Ref("health.email_down"), Detail: detail})
			}
			return out
		},
	}, 5*time.Minute, logger, func(p health.Problem, ok bool) {
		// A door brought up to date needs no "fixed" message.
		if srv.Push == nil || (ok && strings.HasPrefix(p.Key, "door-update:")) {
			return
		}
		// In the board's language: it goes to every sysop's devices.
		lang := i18n.Fallback
		if c, err := config.Load(cfg.BBSConfigPath); err == nil && i18n.Valid(c.BBS.Language) {
			lang = c.BBS.Language
		}
		title := i18n.Resolve(lang, p.Title)
		mark := "⚠ "
		if strings.HasPrefix(p.Key, "door-update:") {
			mark = "⬆ "
		}
		n := push.Notification{Title: mark + title, Body: i18n.Resolve(lang, p.Detail), URL: "/admin/dashboard", Tag: "health-" + p.Key}
		if ok {
			n = push.Notification{Title: i18n.T(lang, "health.fixed", "TITLE", title), URL: "/admin/dashboard", Tag: "health-" + p.Key}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Push.ToSysops(ctx, sqlDB, n); err != nil {
			logger.Warn("health notification: %v", err)
		}
	})

	logger.Info("web admin API listening on %s", cfg.Addr)
	// No write timeout: downloads and the terminal's WebSocket run long.
	httpSrv := &http.Server{Addr: cfg.Addr, Handler: srv.Routes(), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 2 * time.Minute}
	logger.Fatal("%v", httpSrv.ListenAndServe())
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

// migrateDoorConsoles moves doors set up from a template that now plays
// over the socket with a console (Usurper Reborn from 1.2: on standard
// I/O it no longer echoes what's typed) off standard I/O. Once; the
// sysop can still set them back in the admin.
func migrateDoorConsoles(path, dataDir string, c *config.Config, logger *applog.Logger) error {
	var moved []string
	for i := range c.Doors {
		d := &c.Doors[i]
		dir := d.Dir
		t, ok := doors.TemplateFor(d.Template, dir)
		if !ok || !t.Console || !d.Stdio || d.Kind != "" || d.Template != t.ID {
			continue
		}
		d.Stdio, d.Console = false, true
		moved = append(moved, d.Name)
	}
	if len(moved) == 0 {
		return nil
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s for its backup: %w", path, err)
	}
	backup := filepath.Join(dataDir, filepath.Base(path)+".bak-doorconsole-"+time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backup, old, 0o644); err != nil {
		return fmt.Errorf("backing up %s: %w", path, err)
	}
	if err := config.Save(path, c); err != nil {
		return err
	}
	logger.Info("door(s) %s now played over the socket with a console instead of standard I/O; previous config kept as %s",
		strings.Join(moved, ", "), filepath.Base(backup))
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
