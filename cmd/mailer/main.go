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
	"net"
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

	if cfg.Binkp.ListenEnabled && cfg.Binkp.ListenAddr != "" {
		if err := startInboundListener(ctx, cfg, netmailStore, messages, users, logger); err != nil {
			logger.Warn("starting inbound BinkP listener on %s: %v", cfg.Binkp.ListenAddr, err)
		}
	}

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

// inboundSessionTimeout bounds one inbound BinkP session -- generous
// compared to dialTimeout's outbound 60s (unexported in package
// tosser) since we don't control how promptly a caller sends its
// M_ADR/M_PWD or how large a packet it pushes.
const inboundSessionTimeout = 5 * time.Minute

// startInboundListener opens cfg.Binkp.ListenAddr and, in the
// background, accepts and handles inbound BinkP connections (a caller
// dialing us -- see tosser.Answer) until ctx is cancelled. Each
// connection is authenticated by matching its claimed FTN address
// against cfg.Binkp.Uplinks, so only an already-configured uplink can
// push mail to us; anything else is rejected.
func startInboundListener(ctx context.Context, cfg *config.Config, netmailStore *netmail.Store, messages *message.Store, users *user.Store, logger *applog.Logger) error {
	ln, err := net.Listen("tcp", cfg.Binkp.ListenAddr)
	if err != nil {
		return err
	}
	logger.Info("inbound BinkP listener on %s", cfg.Binkp.ListenAddr)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return // listener closed for shutdown -- not an error
				}
				logger.Warn("inbound BinkP listener: accept: %v", err)
				continue
			}
			go handleInboundConn(ctx, conn, cfg, netmailStore, messages, users, logger)
		}
	}()

	return nil
}

// handleInboundConn answers one inbound BinkP connection and logs the
// outcome, mirroring checkUplinks' logging for an outbound poll.
func handleInboundConn(ctx context.Context, conn net.Conn, cfg *config.Config, netmailStore *netmail.Store, messages *message.Store, users *user.Store, logger *applog.Logger) {
	defer conn.Close()

	sessionCtx, cancel := context.WithTimeout(ctx, inboundSessionTimeout)
	defer cancel()

	remote := conn.RemoteAddr().String()
	res, err := tosser.Answer(sessionCtx, conn, cfg.BBS.FTNAddresses, cfg.Binkp.Uplinks, netmailStore, messages, users)
	if err != nil {
		logger.Warn("inbound BinkP session from %s: %v", remote, err)
		return
	}
	logger.Info("inbound BinkP session from %s (%v): received %d netmail, %d echomail%s", remote, res.RemoteAddresses, res.Received, res.ReceivedEcho, skippedFilesSuffix(res.SkippedFiles))
}

// checkUplinks visits every configured uplink once: a crash-only one
// (PollDisabled) is dialed only if Crash-flagged mail is actually
// routed to it right now; any other is dialed only once its own (or
// the global default) poll interval has actually elapsed since it was
// last tried, tracked persistently so a restart can't reset the
// clock. One uplink's failure doesn't stop the others from being
// tried on a later tick.
//
// At most one uplink is actually dialed per call, and none at all if
// any uplink (any of them, not just the one about to be dialed) was
// dialed within minGapBetweenAnyPolls -- a hub's own polling-rate
// limit is often per-IP rather than per-address, so two DIFFERENT
// addresses on the SAME hub can trip it just as easily as polling one
// address twice in a row. Observed live: enabling regular polling for
// two addresses on the same hub (fsxNet and HobbyNet, both on
// dege.au) at once made them due on the very same tick right after
// cmd/mailer's restart, and both got rejected with "Polling too
// frequently" a second apart. Spreading dials out at most one per
// checkInterval tick, plus this explicit gap, keeps that from
// recurring even right after enabling several uplinks together.
func checkUplinks(ctx context.Context, cfg *config.Config, netmailStore *netmail.Store, messages *message.Store, users *user.Store, pollStore *tosser.UplinkPollStore, defaultInterval time.Duration, logger *applog.Logger) {
	if len(cfg.BBS.FTNAddresses) == 0 {
		logger.Warn("this system's FTN address isn't configured; skipping poll")
		return
	}

	now := time.Now()
	var mostRecentPoll time.Time
	for _, uplink := range cfg.Binkp.Uplinks {
		last, err := pollStore.LastPolledAt(uplink.Host)
		if err != nil {
			logger.Warn("checking last poll time for %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}
		if last.After(mostRecentPoll) {
			mostRecentPoll = last
		}
	}
	if !mostRecentPoll.IsZero() && now.Sub(mostRecentPoll) < minGapBetweenAnyPolls {
		return
	}

	for _, uplink := range cfg.Binkp.Uplinks {
		if uplink.PollDisabled {
			if dialedCrashUplink(ctx, cfg, uplink, netmailStore, messages, users, pollStore, logger) {
				return
			}
			continue
		}

		last, err := pollStore.LastPolledAt(uplink.Host)
		if err != nil {
			logger.Warn("checking last poll time for %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}
		if !tosser.IsDue(uplink, last, now, defaultInterval) {
			continue
		}
		if err := pollStore.RecordAttempt(uplink.Host); err != nil {
			logger.Warn("recording poll attempt for %s (%s): %v", uplink.Address, uplink.Host, err)
			continue
		}

		res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, uplink, cfg.Binkp.Uplinks, netmailStore, messages, users)
		if err != nil {
			logger.Warn("polling %s (%s): %v", uplink.Address, uplink.Host, err)
			return
		}
		logger.Info("polled %s (%s): sent %d, received %d netmail, %d echomail%s", uplink.Address, uplink.Host, res.Sent, res.Received, res.ReceivedEcho, skippedFilesSuffix(res.SkippedFiles))
		return
	}
}

// minGapBetweenAnyPolls is the minimum time between dialing any two
// uplinks, regardless of which ones -- see checkUplinks' doc comment.
const minGapBetweenAnyPolls = 5 * time.Minute

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

// dialedCrashUplink dials a crash-only uplink immediately, but only if
// there's actually Crash-flagged mail routed to it right now -- the
// whole point of marking a message Crash is not waiting around for
// the next scheduled poll, but a crash-only uplink still shouldn't be
// dialed needlessly. Reports whether it actually dialed (whether or
// not that dial succeeded), so checkUplinks' caller can stop after one
// dial per tick the same way it does for a regular poll.
func dialedCrashUplink(ctx context.Context, cfg *config.Config, uplink config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store, pollStore *tosser.UplinkPollStore, logger *applog.Logger) bool {
	routed, err := tosser.RoutedOutbound(netmailStore, uplink, cfg.Binkp.Uplinks)
	if err != nil {
		logger.Warn("checking crash mail for %s (%s): %v", uplink.Address, uplink.Host, err)
		return false
	}
	if len(routed) == 0 {
		return false
	}
	if err := pollStore.RecordAttempt(uplink.Host); err != nil {
		logger.Warn("recording poll attempt for %s (%s): %v", uplink.Address, uplink.Host, err)
		return false
	}
	res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, uplink, cfg.Binkp.Uplinks, netmailStore, messages, users)
	if err != nil {
		logger.Warn("crash-dialing %s (%s): %v", uplink.Address, uplink.Host, err)
		return true
	}
	logger.Info("crash-dialed %s (%s): sent %d, received %d netmail, %d echomail%s", uplink.Address, uplink.Host, res.Sent, res.Received, res.ReceivedEcho, skippedFilesSuffix(res.SkippedFiles))
	return true
}
