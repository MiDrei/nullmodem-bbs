package web

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/backup"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/health"
	"github.com/midrei/nullmodem-bbs/internal/i18n"
	"github.com/midrei/nullmodem-bbs/internal/offsite"
	"github.com/midrei/nullmodem-bbs/internal/version"
)

// processStart is when this daemon started, for
// nullmodem_start_time_seconds.
var processStart = time.Now()

// handleMetrics: GET /metrics -- what the dashboard knows, in the
// Prometheus text format, for a monitoring server scraping it. Not
// behind the JWT login: it needs "Authorization: Bearer <token>" with
// web.yaml's metrics_token (or NULLMODEM_METRICS_TOKEN), and answers
// 404 while no token is set.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.MetricsToken)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var b bytes.Buffer
	if err := s.writeMetrics(&b, time.Now()); err != nil {
		s.logWarn("metrics: %v", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = b.WriteTo(w) // the scraper may be gone
}

type metricsWriter struct {
	b    *bytes.Buffer
	seen map[string]bool
}

// metric writes one sample; the HELP and TYPE lines go before the
// first sample of a name. labels are name, value pairs.
func (w *metricsWriter) metric(name, kind, help string, value float64, labels ...string) {
	if !w.seen[name] {
		w.seen[name] = true
		fmt.Fprintf(w.b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}
	w.b.WriteString(name)
	if len(labels) > 0 {
		w.b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				w.b.WriteByte(',')
			}
			fmt.Fprintf(w.b, "%s=%q", labels[i], metricsLabel(labels[i+1]))
		}
		w.b.WriteByte('}')
	}
	fmt.Fprintf(w.b, " %g\n", value)
}

// metricsLabel keeps a label value to what %q writes in the text
// format: control characters are dropped.
func metricsLabel(v string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, v)
}

// unixOr0 is t in Unix seconds, 0 for the zero time.
func unixOr0(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.Unix())
}

func bool01(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// writeMetrics writes the metrics; an error means the database could
// not be read.
func (s *Server) writeMetrics(b *bytes.Buffer, now time.Time) error {
	if s.DB == nil {
		return fmt.Errorf("no database")
	}
	cfg, err := s.loadBBSConfig()
	if err != nil {
		return fmt.Errorf("loading the config: %w", err)
	}
	w := &metricsWriter{b: b, seen: map[string]bool{}}

	w.metric("nullmodem_build_info", "gauge", "Version of the running web daemon.", 1,
		"version", version.Short(), "commit", version.CommitShort())
	w.metric("nullmodem_start_time_seconds", "gauge", "Start of the web daemon.", float64(processStart.Unix()))

	// The health monitor's problems (recorded every 5 minutes).
	problems, err := health.Current(s.DB)
	if err != nil {
		return err
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].Key < problems[j].Key })
	w.metric("nullmodem_problems", "gauge", "Problems the health monitor sees now.", float64(len(problems)))
	for _, p := range problems {
		w.metric("nullmodem_problem", "gauge", "A current problem, by its key; title in English.", 1,
			"key", p.Key, "title", i18n.Resolve("en", p.Title))
	}
	for _, p := range problems {
		w.metric("nullmodem_problem_since_timestamp_seconds", "gauge", "When a current problem started.", unixOr0(p.Since), "key", p.Key)
	}

	// The daemons and doors' background programs.
	if s.Services != nil {
		list, err := s.Services.List()
		if err != nil {
			return err
		}
		for _, st := range list {
			w.metric("nullmodem_service_up", "gauge", "Daemon or door program with a heartbeat in the last 20 seconds (1) or not (0).", bool01(st.Running()), "service", st.Name)
		}
		for _, st := range list {
			w.metric("nullmodem_service_last_seen_timestamp_seconds", "gauge", "Last heartbeat of a daemon or door program, 0 for never.", unixOr0(st.HeartbeatAt), "service", st.Name)
		}
		for _, st := range list {
			w.metric("nullmodem_service_start_time_seconds", "gauge", "Start of a daemon or door program, 0 for never.", unixOr0(st.StartedAt), "service", st.Name)
		}
	}

	// Callers and accounts.
	nodes, err := s.Nodes.List()
	if err != nil {
		return err
	}
	w.metric("nullmodem_nodes_active", "gauge", "Callers connected over Telnet or SSH.", float64(len(nodes)))
	users, err := s.Users.Count()
	if err != nil {
		return err
	}
	w.metric("nullmodem_users", "gauge", "User accounts.", float64(users))
	pending, err := s.Users.Pending()
	if err != nil {
		return err
	}
	w.metric("nullmodem_users_pending", "gauge", "New accounts waiting for the sysop's approval.", float64(len(pending)))
	if s.Guard != nil {
		if l, err := s.Guard.Lockouts(); err == nil {
			w.metric("nullmodem_lockouts", "gauge", "Addresses locked out now for failed logins.", float64(len(l)))
		}
	}
	if s.Chat != nil {
		if rooms, err := s.Chat.Rooms(); err == nil {
			n := 0
			for _, r := range rooms {
				if r.Paging {
					n++
				}
			}
			w.metric("nullmodem_sysop_pages", "gauge", "Callers waiting in their page room for the sysop.", float64(n))
		}
	}

	// Calls (never deleted, so the counts only grow).
	rows, err := s.DB.Query(`SELECT via, COUNT(*) FROM calls GROUP BY via ORDER BY via`)
	if err != nil {
		return fmt.Errorf("counting calls: %w", err)
	}
	for rows.Next() {
		var via string
		var n int
		if err := rows.Scan(&via, &n); err != nil {
			rows.Close()
			return fmt.Errorf("counting calls: %w", err)
		}
		w.metric("nullmodem_calls_total", "counter", "Logins since the board started, by way in (telnet, ssh, web).", float64(n), "via", via)
	}
	rows.Close()
	var calls24h int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM calls WHERE at >= datetime('now', '-1 day')`).Scan(&calls24h); err != nil {
		return fmt.Errorf("counting calls: %w", err)
	}
	w.metric("nullmodem_calls_24h", "gauge", "Logins in the last 24 hours.", float64(calls24h))

	// Echomail stored: the highest message id ever handed out, so
	// maintenance deleting old messages doesn't make it shrink.
	var echo int64
	s.DB.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'messages'`).Scan(&echo)
	w.metric("nullmodem_echomail_messages_total", "counter", "Echomail messages stored since the board started, posted here or tossed in.", float64(echo))

	// Areas a toss created, waiting for approval.
	if pa, err := s.Messages.PendingAreas(); err == nil {
		w.metric("nullmodem_areas_pending", "gauge", "Areas created by a toss, waiting for the sysop's approval.", float64(len(pa)), "kind", "message")
	}
	if pa, err := s.Files.PendingAreas(); err == nil {
		w.metric("nullmodem_areas_pending", "gauge", "Areas created by a toss, waiting for the sysop's approval.", float64(len(pa)), "kind", "file")
	}

	// Netmail.
	var out, crash, unresolved int
	if err := s.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(crash <> 0), 0) FROM netmail_messages
		WHERE to_user_id IS NULL AND to_address != '' AND sent_at IS NULL`).Scan(&out, &crash); err != nil {
		return fmt.Errorf("counting pending netmail: %w", err)
	}
	w.metric("nullmodem_netmail_pending_outbound", "gauge", "Netmail waiting to go out to an FTN address.", float64(out))
	w.metric("nullmodem_netmail_pending_crash", "gauge", "Of those, crash mail.", float64(crash))
	if s.Netmail != nil {
		if unresolved, err = s.Netmail.CountUnresolvedInbox(); err != nil {
			return err
		}
	}
	w.metric("nullmodem_netmail_unresolved", "gauge", "Inbound netmail to nobody known here.", float64(unresolved))

	// Uplinks and downlinks, from the BinkP sessions kept.
	parse := func(v string) time.Time {
		t, _ := time.Parse(time.RFC3339, v)
		return t
	}
	uplinks := s.uplinkStatuses(cfg)
	for _, u := range uplinks {
		mode := "poll"
		switch {
		case u.Downlink:
			mode = "downlink"
		case u.Hold:
			mode = "hold"
		case u.PollDisabled:
			mode = "crash_only"
		}
		w.metric("nullmodem_uplink_info", "gauge", "A configured link: mode is poll, crash_only (called only for mail), hold (never called) or downlink.", 1,
			"uplink", u.Address, "network", u.Network, "host", u.Host, "mode", mode)
	}
	for _, u := range uplinks {
		w.metric("nullmodem_uplink_last_success_timestamp_seconds", "gauge", "Last BinkP session that went through, either way; 0 for never.", unixOr0(parse(u.LastOK)), "uplink", u.Address)
	}
	for _, u := range uplinks {
		w.metric("nullmodem_uplink_last_error_timestamp_seconds", "gauge", "Last BinkP session that failed; 0 for never.", unixOr0(parse(u.LastError)), "uplink", u.Address)
	}
	for _, u := range uplinks {
		w.metric("nullmodem_uplink_failing", "gauge", "The last session failed (1) or went through (0).", bool01(parse(u.LastError).After(parse(u.LastOK))), "uplink", u.Address)
	}
	for _, u := range uplinks {
		w.metric("nullmodem_uplink_sessions_24h", "gauge", "BinkP sessions in the last 24 hours.", float64(u.Sessions24h), "uplink", u.Address)
	}
	for _, u := range uplinks {
		w.metric("nullmodem_uplink_errors_24h", "gauge", "Failed BinkP sessions in the last 24 hours.", float64(u.Errors24h), "uplink", u.Address)
	}

	s.writeBackupMetrics(w, cfg)

	// Space.
	dataDir := cfg.Backup.Directory()
	if s.DBPath != "" {
		dataDir = filepath.Dir(s.DBPath)
		var size int64
		for _, suffix := range []string{"", "-wal"} {
			if fi, err := os.Stat(s.DBPath + suffix); err == nil {
				size += fi.Size()
			}
		}
		w.metric("nullmodem_db_size_bytes", "gauge", "The database with its WAL file.", float64(size))
	}
	if free, total := DiskUsage(dataDir); total > 0 {
		w.metric("nullmodem_disk_free_bytes", "gauge", "Free space on the data volume.", float64(free))
		w.metric("nullmodem_disk_size_bytes", "gauge", "Size of the data volume.", float64(total))
	}
	return nil
}

// writeBackupMetrics: the nightly backups (target local) and their
// off-site copies (target offsite).
func (s *Server) writeBackupMetrics(w *metricsWriter, cfg *config.Config) {
	const help = "Backups switched on (1) or off (0)."
	var newest time.Time
	list, _ := backup.List(cfg.Backup.Directory())
	for _, b := range list {
		if b.Time.After(newest) {
			newest = b.Time
		}
	}
	check, checked := backup.LastCheck(s.DB)
	st := offsite.LoadStatus(s.DB)

	w.metric("nullmodem_backup_enabled", "gauge", help, bool01(cfg.Backup.On()), "target", "local")
	w.metric("nullmodem_backup_enabled", "gauge", help, bool01(cfg.Backup.Offsite.Enabled), "target", "offsite")
	w.metric("nullmodem_backup_last_success_timestamp_seconds", "gauge", "Newest backup (local) or last off-site copy that went through; 0 for none.", unixOr0(newest), "target", "local")
	w.metric("nullmodem_backup_last_success_timestamp_seconds", "gauge", "Newest backup (local) or last off-site copy that went through; 0 for none.", unixOr0(st.LastOK), "target", "offsite")
	w.metric("nullmodem_backup_last_attempt_timestamp_seconds", "gauge", "Last off-site run, whatever came of it; 0 for none.", unixOr0(st.LastTry), "target", "offsite")
	// Local: a backup there and its last check not failed. Off-site:
	// the last run went through.
	w.metric("nullmodem_backup_success", "gauge", "Local: a backup exists and its last check passed. Offsite: the last run went through. 1 yes, 0 no.",
		bool01(!newest.IsZero() && (!checked || check.OK)), "target", "local")
	w.metric("nullmodem_backup_success", "gauge", "Local: a backup exists and its last check passed. Offsite: the last run went through. 1 yes, 0 no.",
		bool01(!st.LastOK.IsZero() && st.LastError == ""), "target", "offsite")
	w.metric("nullmodem_backups", "gauge", "Backups kept (local) or copies there after the last off-site run.", float64(len(list)), "target", "local")
	w.metric("nullmodem_backups", "gauge", "Backups kept (local) or copies there after the last off-site run.", float64(st.Remote), "target", "offsite")
	if checked {
		w.metric("nullmodem_backup_check_timestamp_seconds", "gauge", "Last check of the newest backup (unpacked and read).", unixOr0(check.At))
		w.metric("nullmodem_backup_check_success", "gauge", "The last backup check passed (1) or not (0).", bool01(check.OK))
	}
}
