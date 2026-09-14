// Command mailer runs the BinkP mailer daemon: on a fixed, frequent
// check, it polls whichever configured uplink is actually due (each
// uplink can set its own interval -- some hubs only permit polling
// every hour or two) to send queued netmail and pick up anything
// waiting for us, and dials a crash-only uplink immediately once
// there's Crash-flagged mail routed to it rather than waiting on any
// interval. It shares its database and BBS config file with cmd/bbs
// and cmd/web, but runs as its own process so it can be deployed,
// restarted, and scaled independently -- e.g. as its own
// container/service in docker-compose.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/applog"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/tosser"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// checkInterval is how often the daemon checks which uplinks are due
// -- not how often any single uplink is actually dialed, which is
// governed per-uplink by tosser.IsDue against its own (or the global
// default) poll interval, persisted across restarts in
// tosser.UplinkPollStore. Frequent and cheap: each tick is just a
// couple of local database reads unless something is actually due.
const checkInterval = 60 * time.Second

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
	messages := message.NewStore(sqlDB)
	users := user.NewStore(sqlDB)
	pollStore := tosser.NewUplinkPollStore(sqlDB)

	if len(cfg.Binkp.Uplinks) == 0 {
		logger.Info("no BinkP uplinks configured; idling until the config changes and the daemon is restarted")
	}

	defaultInterval := time.Duration(cfg.Binkp.PollIntervalSeconds) * time.Second
	if defaultInterval <= 0 {
		defaultInterval = 15 * time.Minute
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("mailer daemon starting, checking every %s which uplinks are due (default interval %s)", checkInterval, defaultInterval)
	checkUplinks(ctx, cfg, netmailStore, messages, users, pollStore, defaultInterval, logger)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("mailer daemon shutting down")
			return
		case <-ticker.C:
			checkUplinks(ctx, cfg, netmailStore, messages, users, pollStore, defaultInterval, logger)
		}
	}
}

// checkUplinks visits every configured uplink once: a crash-only one
// (PollDisabled) is dialed only if Crash-flagged mail is actually
// routed to it right now; any other is dialed only once its own (or
// the global default) poll interval has actually elapsed since it was
// last tried, tracked persistently so a restart can't reset the
// clock. One uplink's failure doesn't stop the others from being
// tried.
func checkUplinks(ctx context.Context, cfg *config.Config, netmailStore *netmail.Store, messages *message.Store, users *user.Store, pollStore *tosser.UplinkPollStore, defaultInterval time.Duration, logger *applog.Logger) {
	if len(cfg.BBS.FTNAddresses) == 0 {
		logger.Warn("this system's FTN address isn't configured; skipping poll")
		return
	}
	for _, uplink := range cfg.Binkp.Uplinks {
		if uplink.PollDisabled {
			checkCrashUplink(ctx, cfg, uplink, netmailStore, messages, users, logger)
			continue
		}

		last, err := pollStore.LastPolledAt(uplink.Host)
		if err != nil {
			logger.Warn("checking last poll time for %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}
		if !tosser.IsDue(uplink, last, time.Now(), defaultInterval) {
			continue
		}
		if err := pollStore.RecordAttempt(uplink.Host); err != nil {
			logger.Warn("recording poll attempt for %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}

		res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, uplink, cfg.Binkp.Uplinks, netmailStore, messages, users)
		if err != nil {
			logger.Warn("polling %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}
		logger.Info("polled %s (%s): sent %d, received %d netmail, %d echomail%s", uplink.Address, uplink.Host, res.Sent, res.Received, res.ReceivedEcho, skippedFilesSuffix(res.SkippedFiles))
	}
}

// skippedFilesSuffix formats a log-line suffix noting any inbound
// files tosser.Poll couldn't process (see tosser.Result.SkippedFiles)
// -- empty when there were none, so it doesn't clutter the common
// case.
func skippedFilesSuffix(skipped []string) string {
	if len(skipped) == 0 {
		return ""
	}
	return fmt.Sprintf(", skipped %d unsupported file(s): %s", len(skipped), strings.Join(skipped, ", "))
}

// checkCrashUplink dials a crash-only uplink immediately, but only if
// there's actually Crash-flagged mail routed to it right now -- the
// whole point of marking a message Crash is not waiting around for
// the next scheduled poll, but a crash-only uplink still shouldn't be
// dialed needlessly.
func checkCrashUplink(ctx context.Context, cfg *config.Config, uplink config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store, logger *applog.Logger) {
	routed, err := tosser.RoutedOutbound(netmailStore, uplink, cfg.Binkp.Uplinks)
	if err != nil {
		logger.Warn("checking crash mail for %s (%s): %v", uplink.Address, uplink.Host, err)
		return
	}
	if len(routed) == 0 {
		return
	}
	res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, uplink, cfg.Binkp.Uplinks, netmailStore, messages, users)
	if err != nil {
		logger.Warn("crash-dialing %s (%s): %v", uplink.Address, uplink.Host, err)
		return
	}
	logger.Info("crash-dialed %s (%s): sent %d, received %d netmail, %d echomail%s", uplink.Address, uplink.Host, res.Sent, res.Received, res.ReceivedEcho, skippedFilesSuffix(res.SkippedFiles))
}
