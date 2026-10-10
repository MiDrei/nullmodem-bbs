// Command mailer runs the BinkP mailer daemon: on a fixed, frequent
// check, it dials any uplink that has netmail or echomail routed to
// it right now -- "crash" delivery, immediate rather than waiting on
// any interval, applying to ordinary mail just as much as
// Crash-flagged mail -- throttled so no uplink is dialed more than
// once per minGapBetweenAnyPolls. Absent any pending mail, it falls
// back to polling whichever configured uplink is actually due (each
// uplink can set its own interval -- some hubs only permit polling
// every hour or two) to pick up anything waiting for us. It shares
// its database and BBS config file with cmd/bbs and cmd/web, but runs
// as its own process so it can be deployed, restarted, and scaled
// independently -- e.g. as its own container/service in
// docker-compose.
package main

import (
	"github.com/midrei/nullmodem-bbs/internal/nodelist"
	// Time zones built in: TZ (e.g. Europe/Zurich) works whether the
	// image has a zoneinfo database or not.
	_ "time/tzdata"

	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/midrei/nullmodem-bbs/internal/maintenance"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/applog"
	"github.com/midrei/nullmodem-bbs/internal/archive"
	"github.com/midrei/nullmodem-bbs/internal/areafix"
	"github.com/midrei/nullmodem-bbs/internal/binkplog"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-bbs/internal/message"
	"github.com/midrei/nullmodem-bbs/internal/netmail"
	"github.com/midrei/nullmodem-bbs/internal/proxied"
	"github.com/midrei/nullmodem-bbs/internal/services"
	"github.com/midrei/nullmodem-bbs/internal/tosser"
	"github.com/midrei/nullmodem-bbs/internal/user"
	"github.com/midrei/nullmodem-bbs/internal/version"
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
	files := file.NewStore(sqlDB, cfg.BBS.FilesDir)
	users := user.NewStore(sqlDB)
	if n, err := cfg.ApplyNetworkRenames(messages, files); err != nil {
		logger.Fatal("renaming area groups to network names: %v", err)
	} else if n > 0 {
		logger.Info("renamed %d area group(s) to their network's short name", n)
	}
	pollStore := tosser.NewUplinkPollStore(sqlDB)
	archiveDir := filepath.Join(filepath.Dir(cfg.Database.Path), "inbound-archive")
	sessionLogDir := filepath.Join(filepath.Dir(cfg.Database.Path), "binkp-sessions")
	sessionLog := binkplog.NewStore(sqlDB, sessionLogDir)
	robot := &tosser.RobotConfig{
		OurAddresses: cfg.BBS.FTNAddresses,
		BBSName:      cfg.BBS.Name,
		Sysop:        cfg.BBS.Sysop,
		Location:     cfg.BBS.Location,
		Uplinks:      cfg.Binkp.Uplinks,
		EchoStore:    areafix.NewEchoStore(sqlDB),
		FileStore:    areafix.NewFileStore(sqlDB),
		Files:        files,
		Archive:      archive.NewStore(sqlDB, archiveDir),
	}
	ticCfg := &tosser.TICConfig{Files: files}

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
		if err := startInboundListener(ctx, cfg, netmailStore, messages, users, robot, ticCfg, sessionLog, logger); err != nil {
			logger.Warn("starting inbound BinkP listener on %s: %v", cfg.Binkp.ListenAddr, err)
		}
	}

	// A restart asked for in the web admin (see internal/services) is
	// taken between two rounds of polls, never in the middle of one.
	restartCh := make(chan struct{})
	if inst, err := services.NewStore(sqlDB).Register(services.Mailer, version.ShortBuild()); err != nil {
		logger.Warn("registering with the service list: %v", err)
	} else {
		go inst.Run(ctx, func(string) { close(restartCh) })
	}

	// Nodelists (internal/nodelist): a new one arrives as a file echo
	// now and then; it's imported once there.
	go func() {
		nodelists := nodelist.NewStore(sqlDB)
		tick := time.NewTicker(10 * time.Minute)
		defer tick.Stop()
		for {
			if _, err := nodelists.Sync(false, logger); err != nil {
				logger.Warn("nodelist: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()

	// The nightly cleanup (internal/maintenance), by the config as it
	// is each night.
	maintenance.ApplyLimits(cfg.Maintenance)
	go maintenance.Nightly(ctx, func() config.MaintenanceConfig {
		c, err := config.Load(*configPath)
		if err != nil {
			return cfg.Maintenance
		}
		return c.Maintenance
	}, maintenance.Deps{
		DB:         sqlDB,
		Files:      files,
		Logs:       applog.NewStore(sqlDB),
		SessionLog: sessionLog,
		Archive:    robot.Archive,
		DBPath:     cfg.Database.Path,
	}, logger)

	logger.Info("mailer daemon starting, checking every %s which uplinks are due (default interval %s)", checkInterval, defaultInterval)
	checkUplinks(ctx, cfg, netmailStore, messages, users, robot, ticCfg, pollStore, defaultInterval, sessionLog, logger)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("mailer daemon shutting down")
			return
		case <-restartCh:
			waitForInbound(logger)
			logger.Info("mailer daemon restarting, as asked in the web admin")
			return
		case <-ticker.C:
			checkUplinks(ctx, cfg, netmailStore, messages, users, robot, ticCfg, pollStore, defaultInterval, sessionLog, logger)
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
func startInboundListener(ctx context.Context, cfg *config.Config, netmailStore *netmail.Store, messages *message.Store, users *user.Store, robot *tosser.RobotConfig, ticCfg *tosser.TICConfig, sessionLog *binkplog.Store, logger *applog.Logger) error {
	ln, err := net.Listen("tcp", cfg.Binkp.ListenAddr)
	if err != nil {
		return err
	}
	logger.Info("inbound BinkP listener on %s", cfg.Binkp.ListenAddr)
	listeners := []net.Listener{ln}
	if cfg.Binkp.ProxyListenAddr != "" {
		pln, err := proxied.Listen(cfg.Binkp.ProxyListenAddr)
		if err != nil {
			ln.Close()
			return fmt.Errorf("proxy port %s: %w", cfg.Binkp.ProxyListenAddr, err)
		}
		logger.Info("inbound BinkP listener for a proxy (PROXY protocol) on %s", cfg.Binkp.ProxyListenAddr)
		listeners = append(listeners, pln)
	}

	go func() {
		<-ctx.Done()
		for _, l := range listeners {
			l.Close()
		}
	}()

	for _, ln := range listeners {
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
				activeInbound.Add(1)
				go func() {
					defer activeInbound.Add(-1)
					handleInboundConn(ctx, conn, cfg, netmailStore, messages, users, robot, ticCfg, sessionLog, logger)
				}()
			}
		}()
	}

	return nil
}

// handleInboundConn answers one inbound BinkP connection and logs the
// outcome, mirroring checkUplinks' logging for an outbound poll.
func handleInboundConn(ctx context.Context, conn net.Conn, cfg *config.Config, netmailStore *netmail.Store, messages *message.Store, users *user.Store, robot *tosser.RobotConfig, ticCfg *tosser.TICConfig, sessionLog *binkplog.Store, logger *applog.Logger) {
	defer conn.Close()

	sessionCtx, cancel := context.WithTimeout(ctx, inboundSessionTimeout)
	defer cancel()

	remote := conn.RemoteAddr().String()
	res, err := tosser.Answer(sessionCtx, conn, cfg.BBS.FTNAddresses, cfg.BBS.Name, cfg.Binkp.Uplinks, netmailStore, messages, users, robot, ticCfg, sessionLog)
	if err != nil {
		logger.Warn("inbound BinkP session from %s: %v", remote, err)
		return
	}
	if res.Unsecured {
		// A nodelist checker and the like: nothing exchanged.
		logger.Info("inbound BinkP check from %s (%v, %q): unlisted, answered without exchanging mail", remote, res.RemoteAddresses, res.PeerSystem)
		return
	}
	logger.Info("inbound BinkP session from %s (%v): sent %d netmail, %d echomail, forwarded %d echomail, %d file(s), received %d netmail, %d echomail, %d file(s)%s", remote, res.RemoteAddresses, res.Sent, res.SentEcho, res.ForwardedEcho, res.ForwardedFiles, res.Received, res.ReceivedEcho, res.ReceivedFiles, skippedFilesSuffix(res.SkippedFiles)+replacedSuffix(res.ReplacedFiles))
}

// checkUplinks visits every configured uplink once: a Hold uplink
// (see config.BinkpUplink.Hold) is skipped entirely here -- it's
// never dialed automatically for any reason, only via "Send Now".
// Otherwise, any uplink -- crash-only (PollDisabled) or not -- is
// dialed immediately if any netmail or echomail is actually routed to
// it right now (see dialedForPendingMail), regardless of whether that
// mail is Crash-flagged. Only once an uplink has nothing pending does
// it fall
// back to slower scheduling: a crash-only uplink gets a fallback poll
// if it has its own explicit PollIntervalSeconds set (never the
// global default: an uplink that wants this must say so explicitly,
// or it stays purely crash-only, picking up mail only when something
// becomes pending or it pushes to us the way lovlynet's inbound
// BinkP listener does), and any other uplink is dialed once its own
// (or the global default) poll interval has actually elapsed since it
// was last tried, tracked persistently so a restart can't reset the
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
// recurring even right after enabling several uplinks together --
// and, since checkInterval is 60s and minGapBetweenAnyPolls is 5
// minutes, this same gap is what throttles crash delivery to at most
// once per 5 minutes even when mail keeps arriving faster than that.
func checkUplinks(ctx context.Context, cfg *config.Config, netmailStore *netmail.Store, messages *message.Store, users *user.Store, robot *tosser.RobotConfig, ticCfg *tosser.TICConfig, pollStore *tosser.UplinkPollStore, defaultInterval time.Duration, sessionLog *binkplog.Store, logger *applog.Logger) {
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
		if uplink.Hold {
			continue // never dialed automatically -- see BinkpUplink.Hold's own doc comment
		}
		if dialedForPendingMail(ctx, cfg, uplink, netmailStore, messages, users, robot, ticCfg, pollStore, sessionLog, logger) {
			return
		}
		if uplink.PollDisabled {
			if uplink.PollIntervalSeconds <= 0 {
				continue // purely crash-only: no fallback interval configured
			}
		}
		if pollIfDue(ctx, cfg, uplink, netmailStore, messages, users, robot, ticCfg, pollStore, defaultInterval, now, sessionLog, logger) {
			return
		}
	}
}

// pollIfDue dials uplink if its own (or, for a uplink that doesn't
// set PollIntervalSeconds, the global default) interval has actually
// elapsed since it was last tried, recording the attempt and logging
// the outcome. Reports whether it actually dialed (whether or not
// that dial succeeded), so checkUplinks' caller can stop after one
// dial per tick the same way it does for a crash dial.
func pollIfDue(ctx context.Context, cfg *config.Config, uplink config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store, robot *tosser.RobotConfig, ticCfg *tosser.TICConfig, pollStore *tosser.UplinkPollStore, defaultInterval time.Duration, now time.Time, sessionLog *binkplog.Store, logger *applog.Logger) bool {
	last, err := pollStore.LastPolledAt(uplink.Host)
	if err != nil {
		logger.Warn("checking last poll time for %s (%s): %v", uplink.Address, uplink.Host, err)
		return false
	}
	if !tosser.IsDue(uplink, last, now, defaultInterval) {
		return false
	}
	if err := pollStore.RecordAttempt(uplink.Host); err != nil {
		logger.Warn("recording poll attempt for %s (%s): %v", uplink.Address, uplink.Host, err)
		return false
	}

	res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, cfg.BBS.Name, uplink, cfg.Binkp.Uplinks, netmailStore, messages, users, robot, ticCfg, sessionLog)
	if err != nil {
		logger.Warn("polling %s (%s): %v%s", uplink.Address, uplink.Host, err, dialFailed(uplink.Host))
		return true
	}
	dialBackoff.Worked(uplink.Host)
	logger.Info("polled %s (%s): sent %d netmail, %d echomail, forwarded %d echomail, %d file(s), received %d netmail, %d echomail, %d file(s)%s", uplink.Address, uplink.Host, res.Sent, res.SentEcho, res.ForwardedEcho, res.ForwardedFiles, res.Received, res.ReceivedEcho, res.ReceivedFiles, skippedFilesSuffix(res.SkippedFiles)+replacedSuffix(res.ReplacedFiles))
	return true
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

// dialedForPendingMail dials uplink immediately -- "crash" delivery,
// ahead of any scheduled poll -- if it has any netmail or echomail
// routed to it right now, ordinary or Crash-flagged alike (see
// tosser.RoutedOutbound and tosser.RoutedOutboundEcho); otherwise it
// dials nothing, leaving the uplink to checkUplinks' slower fallback
// scheduling. Reports whether it actually dialed (whether or not that
// dial succeeded), so checkUplinks' caller can stop after one dial
// per tick the same way it does for a regular poll. After failed
// dials, it waits longer and longer before the next (see dialBackoff).
func dialedForPendingMail(ctx context.Context, cfg *config.Config, uplink config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store, robot *tosser.RobotConfig, ticCfg *tosser.TICConfig, pollStore *tosser.UplinkPollStore, sessionLog *binkplog.Store, logger *applog.Logger) bool {
	routedNetmail, err := tosser.RoutedOutbound(netmailStore, uplink, cfg.Binkp.Uplinks)
	if err != nil {
		logger.Warn("checking pending netmail for %s (%s): %v", uplink.Address, uplink.Host, err)
		return false
	}
	routedEcho, err := tosser.RoutedOutboundEcho(messages, uplink)
	if err != nil {
		logger.Warn("checking pending echomail for %s (%s): %v", uplink.Address, uplink.Host, err)
		return false
	}
	var forwardedEcho []message.PendingEcho
	var forwardedFiles []tosser.PendingFileForward
	if robot != nil {
		if robot.EchoStore != nil {
			forwardedEcho, err = tosser.RoutedOutboundEchoForward(messages, robot.EchoStore, uplink)
			if err != nil {
				logger.Warn("checking forwarded echomail for %s (%s): %v", uplink.Address, uplink.Host, err)
				return false
			}
		}
		if robot.FileStore != nil && robot.Files != nil {
			forwardedFiles, err = tosser.RoutedOutboundFileForward(robot.Files, robot.FileStore, uplink)
			if err != nil {
				logger.Warn("checking forwarded files for %s (%s): %v", uplink.Address, uplink.Host, err)
				return false
			}
		}
	}
	if len(routedNetmail) == 0 && len(routedEcho) == 0 && len(forwardedEcho) == 0 && len(forwardedFiles) == 0 {
		return false
	}
	if !dialBackoff.Ready(uplink.Host, time.Now()) {
		return false // its last dials failed; the mail waits (or the hub picks it up calling in)
	}
	if err := pollStore.RecordAttempt(uplink.Host); err != nil {
		logger.Warn("recording poll attempt for %s (%s): %v", uplink.Address, uplink.Host, err)
		return false
	}
	res, err := tosser.Poll(ctx, cfg.BBS.FTNAddresses, cfg.BBS.Name, uplink, cfg.Binkp.Uplinks, netmailStore, messages, users, robot, ticCfg, sessionLog)
	if err != nil {
		logger.Warn("crash-dialing %s (%s) for pending mail: %v%s", uplink.Address, uplink.Host, err, dialFailed(uplink.Host))
		return true
	}
	dialBackoff.Worked(uplink.Host)
	logger.Info("crash-dialed %s (%s) for pending mail: sent %d netmail, %d echomail, forwarded %d echomail, %d file(s), received %d netmail, %d echomail, %d file(s)%s", uplink.Address, uplink.Host, res.Sent, res.SentEcho, res.ForwardedEcho, res.ForwardedFiles, res.Received, res.ReceivedEcho, res.ReceivedFiles, skippedFilesSuffix(res.SkippedFiles)+replacedSuffix(res.ReplacedFiles))
	return true
}

// dialBackoff holds back crash dials to an uplink whose last dials
// failed -- see tosser.DialBackoff.
var dialBackoff = &tosser.DialBackoff{FirstWait: minGapBetweenAnyPolls, MaxWait: time.Hour}

// dialFailed records a failed dial to host and returns a log-line
// suffix saying when mail waiting for it is tried again, once it
// failed more than once in a row.
func dialFailed(host string) string {
	n := dialBackoff.Failed(host, time.Now())
	if n < 2 {
		return ""
	}
	return fmt.Sprintf(" (failed %d times in a row; waiting mail is tried again in %s)", n, dialBackoff.Wait(host))
}

// activeInbound counts inbound BinkP sessions in progress, so a
// requested restart can let them finish first.
var activeInbound atomic.Int32

// waitForInbound waits for inbound sessions in progress to end -- at
// most inboundSessionTimeout, which bounds every one of them anyway.
func waitForInbound(logger *applog.Logger) {
	deadline := time.Now().Add(inboundSessionTimeout)
	if activeInbound.Load() > 0 {
		logger.Info("restart asked for: waiting for %d inbound BinkP session(s) to finish", activeInbound.Load())
	}
	for activeInbound.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Second)
	}
}

// replacedSuffix notes files a TIC "Replaces" line deleted, if any.
func replacedSuffix(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(", replaced %d old file(s)", n)
}
