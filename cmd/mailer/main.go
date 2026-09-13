// Command mailer runs the BinkP mailer daemon: it periodically polls
// every configured, non-crash-only uplink (internal/tosser) to send
// queued netmail and pick up anything waiting for us, and separately
// watches for Crash-flagged mail that needs a crash-only uplink
// dialed immediately instead of waiting for that schedule. It shares
// its database and BBS config file with cmd/bbs and cmd/web, but runs
// as its own process so it can be deployed, restarted, and scaled
// independently -- e.g. as its own container/service in
// docker-compose.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/applog"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/tosser"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// crashCheckInterval is how often the crash-check loop looks for
// Crash-flagged mail waiting on a crash-only (PollDisabled) uplink --
// much faster than the regular poll interval, since the entire point
// of Crash mail is not waiting around for it. This only ever queries
// the local database (see tosser.RoutedOutbound); it dials out only
// when there's actually something routed to send.
const crashCheckInterval = 30 * time.Second

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

	logger := applog.NewLogger(applog.NewStore(sqlDB), "mailer")
	netmailStore := netmail.NewStore(sqlDB)
	users := user.NewStore(sqlDB)

	if len(cfg.Binkp.Uplinks) == 0 {
		logger.Info("no BinkP uplinks configured; idling until the config changes and the daemon is restarted")
	}

	interval := time.Duration(cfg.Binkp.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 15 * time.Minute
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("mailer daemon starting, polling every %s (crash check every %s)", interval, crashCheckInterval)
	pollDue(ctx, cfg, netmailStore, users, logger)

	pollTicker := time.NewTicker(interval)
	defer pollTicker.Stop()
	crashTicker := time.NewTicker(crashCheckInterval)
	defer crashTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("mailer daemon shutting down")
			return
		case <-pollTicker.C:
			pollDue(ctx, cfg, netmailStore, users, logger)
		case <-crashTicker.C:
			pollCrash(ctx, cfg, netmailStore, users, logger)
		}
	}
}

// pollDue polls every configured uplink that isn't crash-only
// (PollDisabled), in order, logging each result -- one uplink's
// failure doesn't stop the others from being tried.
func pollDue(ctx context.Context, cfg *config.Config, netmailStore *netmail.Store, users *user.Store, logger *applog.Logger) {
	if len(cfg.BBS.FTNAddresses) == 0 {
		logger.Warn("this system's FTN address isn't configured; skipping poll")
		return
	}
	for _, uplink := range cfg.Binkp.Uplinks {
		if uplink.PollDisabled {
			continue
		}
		res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, uplink, cfg.Binkp.Uplinks, netmailStore, users)
		if err != nil {
			logger.Warn("polling %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}
		logger.Info("polled %s (%s): sent %d, received %d", uplink.Address, uplink.Host, res.Sent, res.Received)
	}
}

// pollCrash checks every crash-only (PollDisabled) uplink for Crash-
// flagged mail routed to it and, only if there's something actually
// waiting, dials that uplink immediately rather than waiting for the
// next regular poll -- the whole point of marking a message Crash.
func pollCrash(ctx context.Context, cfg *config.Config, netmailStore *netmail.Store, users *user.Store, logger *applog.Logger) {
	if len(cfg.BBS.FTNAddresses) == 0 {
		return
	}
	for _, uplink := range cfg.Binkp.Uplinks {
		if !uplink.PollDisabled {
			continue
		}
		routed, err := tosser.RoutedOutbound(netmailStore, uplink, cfg.Binkp.Uplinks)
		if err != nil {
			logger.Warn("checking crash mail for %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}
		if len(routed) == 0 {
			continue
		}
		res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, uplink, cfg.Binkp.Uplinks, netmailStore, users)
		if err != nil {
			logger.Warn("crash-dialing %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}
		logger.Info("crash-dialed %s (%s): sent %d, received %d", uplink.Address, uplink.Host, res.Sent, res.Received)
	}
}
