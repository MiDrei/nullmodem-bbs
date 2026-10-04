// Package emailgw is the netmail <-> email gateway: every caller who
// may use it has the address handle@domain. Mail to it arrives in a
// (catch-all) mailbox the gateway fetches over IMAP and becomes netmail
// to that caller; netmail a caller writes to an email address goes out
// over SMTP from that address. It runs in the web daemon.
//
// A mail is a netmail row with Email set (see internal/netmail's
// email.go), so the inbox, the reader, replies, QWK and the reader app
// handle it like any netmail.
package emailgw

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/applog"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// Alias is username's address part: lower case, a space a dot, and
// only what an address may hold (SwissMaik -> swissmaik, "Joe Bloggs"
// -> joe.bloggs).
func Alias(username string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(username)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == '.' || r == ' ':
			if s := b.String(); s != "" && !strings.HasSuffix(s, ".") {
				b.WriteByte('.')
			}
		}
	}
	return strings.Trim(b.String(), ".")
}

// Address is u's mail address under cfg, "" without a domain.
func Address(cfg config.EmailConfig, username string) string {
	a := Alias(username)
	if cfg.Domain == "" || a == "" {
		return ""
	}
	return a + "@" + strings.ToLower(cfg.Domain)
}

// May reports whether u may use the gateway: it's on, u is approved
// and has the level.
func May(cfg config.EmailConfig, u *user.User) bool {
	return cfg.Enabled && cfg.Domain != "" && u != nil && u.Validated && u.SecurityLevel >= cfg.MinSL && Alias(u.Username) != ""
}

// Why a caller can't send a mail.
var (
	ErrOff        = errors.New("emailgw: the email gateway is off")
	ErrNotAllowed = errors.New("emailgw: not allowed to send email")
	ErrLimit      = errors.New("emailgw: daily email limit reached")
)

// CheckSend reports whether u may send one more mail now.
func CheckSend(cfg config.EmailConfig, store *netmail.Store, u *user.User, now time.Time) error {
	if !cfg.Enabled || cfg.Domain == "" {
		return ErrOff
	}
	if !May(cfg, u) {
		return ErrNotAllowed
	}
	n, err := store.EmailsSince(u.ID, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if n >= cfg.Limit() {
		return ErrLimit
	}
	return nil
}

// Status is how the gateway is doing, kept in the meta table for the
// web admin.
type Status struct {
	LastFetch      time.Time `json:"last_fetch"`       // last successful fetch
	LastFetchError string    `json:"last_fetch_error"` // of the last try, "" when it worked
	LastSend       time.Time `json:"last_send"`        // last mail sent
	LastSendError  string    `json:"last_send_error"`
	Received       int       `json:"received"` // since the start of the counting
	Sent           int       `json:"sent"`
	Dropped        int       `json:"dropped"` // unknown addresses, spam, not allowed
}

const statusKey = "emailgw_status"

// LoadStatus reads the gateway's status.
func LoadStatus(db *sql.DB) Status {
	var st Status
	var raw string
	if db.QueryRow(`SELECT value FROM meta WHERE key = ?`, statusKey).Scan(&raw) == nil {
		json.Unmarshal([]byte(raw), &st)
	}
	return st
}

func saveStatus(db *sql.DB, st Status) {
	raw, _ := json.Marshal(st)
	db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, statusKey, string(raw))
}

// Gateway moves the mail: fetches the mailbox, sends what's waiting.
type Gateway struct {
	DB      *sql.DB
	Netmail *netmail.Store
	Users   *user.Store
	Logger  *applog.Logger
	// Config is read fresh every round: settings saved in the admin
	// apply at once.
	Config func() config.EmailConfig
	// Lang is the language to write a caller's notices in.
	Lang func(u *user.User) string
	// BBSName goes into the mail's headers.
	BBSName func() string

	mu   sync.Mutex
	wake chan struct{}
}

// FetchEvery is how often the mailbox is fetched; SendEvery how often
// waiting mail is looked for.
const (
	FetchEvery = time.Minute
	SendEvery  = 20 * time.Second
)

// Run moves mail until ctx ends.
func (g *Gateway) Run(ctx context.Context) {
	g.mu.Lock()
	if g.wake == nil {
		g.wake = make(chan struct{}, 1)
	}
	wake := g.wake
	g.mu.Unlock()
	var lastFetch time.Time
	tick := time.NewTicker(SendEvery)
	defer tick.Stop()
	for {
		cfg := g.Config()
		if cfg.Enabled && cfg.Domain != "" {
			g.SendPending(ctx, cfg, time.Now())
			if time.Since(lastFetch) >= FetchEvery {
				lastFetch = time.Now()
				g.Fetch(ctx, cfg)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-wake:
			lastFetch = time.Time{}
		}
	}
}

// Wake runs a round now (settings changed, "fetch now").
func (g *Gateway) Wake() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.wake == nil {
		g.wake = make(chan struct{}, 1)
	}
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

// Down reports a problem worth the sysop's attention: the gateway on,
// but the mailbox not fetched for longer than after, or mail waiting
// longer than after.
func (g *Gateway) Down(after time.Duration) (bool, string) {
	cfg := g.Config()
	if !cfg.Enabled || cfg.Domain == "" {
		return false, ""
	}
	st := LoadStatus(g.DB)
	if st.LastFetchError != "" && time.Since(st.LastFetch) > after {
		return true, st.LastFetchError
	}
	waiting, oldest, _, err := g.Netmail.EmailQueue(time.Now())
	if err == nil && waiting > 0 && !oldest.IsZero() && time.Since(oldest) > after+time.Hour {
		if st.LastSendError != "" {
			return true, st.LastSendError
		}
		return true, ""
	}
	return false, ""
}

func (g *Gateway) update(f func(*Status)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := LoadStatus(g.DB)
	f(&st)
	saveStatus(g.DB, st)
}

func (g *Gateway) logInfo(format string, args ...any) {
	if g.Logger != nil {
		g.Logger.Info(format, args...)
	}
}

func (g *Gateway) logWarn(format string, args ...any) {
	if g.Logger != nil {
		g.Logger.Warn(format, args...)
	}
}

// userByAlias finds the caller whose alias is alias.
func (g *Gateway) userByAlias(alias string) (*user.User, error) {
	all, err := g.Users.ListAll()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if Alias(all[i].Username) == alias {
			return &all[i], nil
		}
	}
	return nil, user.ErrNotFound
}
