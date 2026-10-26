package web

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/i18n"
	"github.com/midrei/nullmodem-bbs/internal/services"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestMetrics(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)

	// Off without a token.
	if rec := doJSON(t, srv.Routes(), http.MethodGet, "/metrics", nil, "anything"); rec.Code != http.StatusNotFound {
		t.Fatalf("no token set: %d, want 404", rec.Code)
	}

	srv.MetricsToken = "s3cret"
	h := srv.Routes()
	if rec := doJSON(t, h, http.MethodGet, "/metrics", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token sent: %d, want 401", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodGet, "/metrics", nil, "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d, want 401", rec.Code)
	}
	// A sysop's login is no metrics token.
	if rec := doJSON(t, h, http.MethodGet, "/metrics", nil, loginAsSysop(t, h, "root", "supersecret")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login token: %d, want 401", rec.Code)
	}

	// Some state to report.
	if _, err := srv.Services.Register(services.Mailer, "1.0"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(configPath)
	cfg.Binkp.Uplinks = []config.BinkpUplink{{Address: "21:3/100", Host: "hub", Network: "fsxnet"}}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	srv.DB.Exec(`INSERT INTO binkp_sessions (direction, peer_address, started_at, storage_path, outcome) VALUES ('outbound', '21:3/100', '2026-10-01 10:00:00', 'x', 'ok')`)
	srv.DB.Exec(`INSERT INTO health_problems (key, title, detail, since) VALUES ('backup', ?, '', ?)`, i18n.Ref("health.backup_overdue"), time.Unix(1700000000, 0).UnixMilli())
	srv.DB.Exec(`INSERT INTO calls (user_id, via) VALUES (1, 'telnet'), (1, 'telnet'), (1, 'web')`)
	srv.DB.Exec(`INSERT INTO netmail_messages (from_name, to_name, to_address, subject, body) VALUES ('a', 'b', '1:2/3', 's', 'b')`)

	rec := doJSON(t, h, http.MethodGet, "/metrics", nil, "s3cret")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE nullmodem_build_info gauge\n",
		"nullmodem_users 1\n",
		"nullmodem_users_pending 0\n",
		"nullmodem_nodes_active 0\n",
		"nullmodem_problems 1\n",
		`nullmodem_problem{key="backup",title="The nightly backup is overdue"} 1` + "\n",
		`nullmodem_problem_since_timestamp_seconds{key="backup"} 1.7e+09` + "\n",
		`nullmodem_service_up{service="mailer"} 1` + "\n",
		`nullmodem_service_up{service="bbs"} 0` + "\n",
		`nullmodem_service_last_seen_timestamp_seconds{service="bbs"} 0` + "\n",
		"# TYPE nullmodem_calls_total counter\n",
		`nullmodem_calls_total{via="telnet"} 2` + "\n",
		`nullmodem_calls_total{via="web"} 1` + "\n",
		"nullmodem_netmail_pending_outbound 1\n",
		"nullmodem_netmail_unresolved 0\n",
		`nullmodem_uplink_info{uplink="21:3/100",network="fsxnet",host="hub",mode="poll"} 1` + "\n",
		`nullmodem_uplink_last_success_timestamp_seconds{uplink="21:3/100"} 1.7908488e+09` + "\n",
		`nullmodem_uplink_last_error_timestamp_seconds{uplink="21:3/100"} 0` + "\n",
		`nullmodem_uplink_failing{uplink="21:3/100"} 0` + "\n",
		`nullmodem_backup_last_success_timestamp_seconds{target="local"} 0` + "\n",
		`nullmodem_backup_success{target="offsite"} 0` + "\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
	if strings.Count(body, "# HELP nullmodem_service_up ") != 1 {
		t.Error("HELP written more than once")
	}
}

func TestMetricsLabelEscape(t *testing.T) {
	var b bytes.Buffer
	w := &metricsWriter{b: &b, seen: map[string]bool{}}
	w.metric("x", "gauge", "h", 1, "k", "a\"b\\c\nd")
	if got, want := b.String(), "# HELP x h\n# TYPE x gauge\n"+`x{k="a\"b\\cd"} 1`+"\n"; got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
