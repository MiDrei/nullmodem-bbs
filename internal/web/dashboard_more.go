package web

import (
	"os"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/applog"
	"github.com/midrei/nullmodem-bbs/internal/backup"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/emailgw"
	"github.com/midrei/nullmodem-bbs/internal/offsite"
)

// uplinkStatusDTO is how the mailer gets on with one uplink, from the
// BinkP sessions kept: when a session last went through, when one last
// failed and why, and how many there were in the last 24 hours.
type uplinkStatusDTO struct {
	Address      string `json:"address"`
	Network      string `json:"network"`
	Host         string `json:"host"`
	Downlink     bool   `json:"downlink"`
	Hold         bool   `json:"hold"`
	PollDisabled bool   `json:"poll_disabled"`
	LastOK       string `json:"last_ok"`
	LastError    string `json:"last_error"`
	Error        string `json:"error"`
	Sessions24h  int    `json:"sessions_24h"`
	Errors24h    int    `json:"errors_24h"`
}

// sessionTime reads binkp_sessions.started_at (UTC, SQLite's format).
func sessionTime(v string) string {
	t, err := time.Parse("2006-01-02 15:04:05", v)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Server) uplinkStatuses(cfg *config.Config) []uplinkStatusDTO {
	out := []uplinkStatusDTO{}
	for _, u := range cfg.Binkp.Uplinks {
		d := uplinkStatusDTO{Address: u.Address, Network: u.Network, Host: u.Host, Downlink: u.Downlink, Hold: u.Hold, PollDisabled: u.PollDisabled}
		if s.DB != nil {
			var ok, bad, detail string
			s.DB.QueryRow(`SELECT COALESCE(strftime('%Y-%m-%d %H:%M:%S', MAX(started_at)), '') FROM binkp_sessions WHERE peer_address = ? AND outcome = 'ok'`, u.Address).Scan(&ok)
			s.DB.QueryRow(`SELECT strftime('%Y-%m-%d %H:%M:%S', started_at), detail FROM binkp_sessions WHERE peer_address = ? AND outcome = 'error' ORDER BY id DESC LIMIT 1`, u.Address).Scan(&bad, &detail)
			// An outbound attempt that never got an address back is kept
			// under the host it dialed.
			var badHost, detailHost string
			s.DB.QueryRow(`SELECT strftime('%Y-%m-%d %H:%M:%S', started_at), detail FROM binkp_sessions WHERE peer_address = '' AND peer_host = ? AND outcome = 'error' ORDER BY id DESC LIMIT 1`, u.Host).Scan(&badHost, &detailHost)
			if badHost > bad {
				bad, detail = badHost, detailHost
			}
			d.LastOK, d.LastError, d.Error = sessionTime(ok), sessionTime(bad), detail
			s.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(outcome = 'error'), 0) FROM binkp_sessions
				WHERE (peer_address = ? OR (peer_address = '' AND peer_host = ?)) AND started_at >= datetime('now', '-1 day')`,
				u.Address, u.Host).Scan(&d.Sessions24h, &d.Errors24h)
		}
		out = append(out, d)
	}
	return out
}

// systemDTO is the board's housekeeping at a glance.
type systemDTO struct {
	DBBytes        int64          `json:"db_bytes"`
	FreeBytes      uint64         `json:"free_bytes"`
	BackupEnabled  bool           `json:"backup_enabled"`
	LastBackup     *backup.Info   `json:"last_backup"`
	BackupCheck    *backup.Check  `json:"backup_check"`
	OffsiteEnabled bool           `json:"offsite_enabled"`
	Offsite        offsite.Status `json:"offsite"`
	Services       []serviceBrief `json:"services"`
	Warnings       []logLineBrief `json:"warnings"`
	Email          *emailBrief    `json:"email"` // nil while the gateway is off
}

// emailBrief is how the email gateway's ways in and out are doing.
type emailBrief struct {
	Server     *emailgw.ReceiveStatus `json:"server"` // the own mail server, if on
	Webhook    bool                   `json:"webhook"`
	Mailbox    bool                   `json:"mailbox"` // fetched over IMAP
	LastFetch  string                 `json:"last_fetch"`
	FetchError string                 `json:"fetch_error"`
	LastIn     string                 `json:"last_in"` // the newest mail taken, by any way
	LastOut    string                 `json:"last_out"`
	SendError  string                 `json:"send_error"`
	Waiting    int                    `json:"waiting"`
	Failed     int                    `json:"failed"` // in the last day
}

type serviceBrief struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	StartedAt string `json:"started_at"`
	Running   bool   `json:"running"`
}

type logLineBrief struct {
	At      string `json:"at"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

func (s *Server) systemStatus(cfg *config.Config) systemDTO {
	d := systemDTO{Services: []serviceBrief{}, Warnings: []logLineBrief{}}
	if s.DBPath != "" {
		for _, suffix := range []string{"", "-wal"} {
			if fi, err := os.Stat(s.DBPath + suffix); err == nil {
				d.DBBytes += fi.Size()
			}
		}
	}
	d.FreeBytes = freeBytes(cfg.Backup.Directory())
	d.BackupEnabled = cfg.Backup.On()
	if list, err := backup.List(cfg.Backup.Directory()); err == nil {
		for i := range list {
			if d.LastBackup == nil || list[i].Time.After(d.LastBackup.Time) {
				d.LastBackup = &list[i]
			}
		}
	}
	d.BackupCheck = s.lastBackupCheck()
	d.OffsiteEnabled = cfg.Backup.Offsite.Enabled
	if s.DB != nil {
		d.Offsite = offsite.LoadStatus(s.DB)
	}
	if s.Services != nil {
		if list, err := s.Services.List(); err == nil {
			for _, st := range list {
				d.Services = append(d.Services, serviceBrief{Name: st.Name, Version: st.Version, StartedAt: iso(st.StartedAt), Running: st.Running()})
			}
		}
	}
	if s.Logs != nil {
		if entries, err := s.Logs.List(applog.Filter{Levels: []applog.Level{applog.LevelWarn, applog.LevelError}, Limit: 5}); err == nil {
			for i := len(entries) - 1; i >= 0; i-- { // newest first
				e := entries[i]
				if time.Since(e.LoggedAt) > 7*24*time.Hour {
					continue
				}
				d.Warnings = append(d.Warnings, logLineBrief{At: iso(e.LoggedAt), Level: string(e.Level), Source: e.Source, Message: e.Message})
			}
		}
	}
	d.Email = s.emailBrief(cfg)
	return d
}

func (s *Server) emailBrief(cfg *config.Config) *emailBrief {
	e := cfg.Email
	if !e.Enabled || s.DB == nil {
		return nil
	}
	st := emailgw.LoadStatus(s.DB)
	b := &emailBrief{Webhook: e.Receive.Webhook, Mailbox: e.IMAP.Host != "",
		LastFetch: iso(st.LastFetch), FetchError: st.LastFetchError, LastOut: iso(st.LastSend), SendError: st.LastSendError}
	if e.Receive.SMTP && s.MailReceiver != nil {
		rs := s.MailReceiver.Status()
		b.Server = &rs
	}
	var in time.Time
	if s.DB.QueryRow(`SELECT posted_at FROM netmail_messages WHERE email != '' AND from_user_id IS NULL ORDER BY id DESC LIMIT 1`).Scan(&in) == nil {
		b.LastIn = iso(in)
	}
	if s.Netmail != nil {
		b.Waiting, _, b.Failed, _ = s.Netmail.EmailQueue(time.Now())
	}
	return b
}
