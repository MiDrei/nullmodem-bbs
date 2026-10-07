// Package guard keeps password guessers out: every failed login (on
// Telnet, SSH, the web portal or the admin) is recorded with the
// caller's IP, and an IP that fails too often in a short while is
// locked out for a time -- longer each time it happens again. The
// sysop's allow list (never locked out) and block list (always) take
// IPs or CIDR ranges. The state is in the shared database, so the bbs
// and web daemons lock out the same addresses.
package guard

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/i18n"
)

// Settings are the lockout limits (config.SecurityConfig).
type Settings struct {
	Enabled bool
	// MaxFailures within Window lock an IP out for Lockout, four
	// times as long for each further lockout within a day, at most
	// MaxLockout.
	MaxFailures int
	Window      time.Duration
	Lockout     time.Duration
	MaxLockout  time.Duration
}

// Logger is what lockouts are reported to.
type Logger interface {
	Warn(format string, args ...any)
}

// Guard records failures and answers whether an IP may log in.
type Guard struct {
	db       *sql.DB
	settings func() Settings
	log      Logger
	now      func() time.Time

	mu        sync.Mutex
	lastPrune time.Time
}

// New returns a guard; settings is asked each time (it may re-read the
// config), log may be nil.
func New(db *sql.DB, settings func() Settings, log Logger) *Guard {
	return &Guard{db: db, settings: settings, log: log, now: time.Now}
}

// IP is addr's address without its port, as stored: "203.0.113.5",
// "2001:db8::1" (IPv4-mapped IPv6 as IPv4).
func IP(addr string) string {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return a.Unmap().String()
	}
	return host
}

// Verdict is whether an IP may try to log in.
type Verdict struct {
	Blocked bool
	// Until is when a lockout ends; zero for a block-list entry.
	Until time.Time
}

// Message is what a refused caller is told.
func (v Verdict) Message() string { return v.MessageIn(i18n.Fallback) }

// MessageIn is Message in lang (an internal/i18n code).
func (v Verdict) MessageIn(lang string) string {
	if v.Until.IsZero() {
		return i18n.T(lang, "guard.blocked")
	}
	return i18n.T(lang, "guard.locked", "TIME", v.Until.Format("15:04"))
}

// Check says whether ip may log in now.
func (g *Guard) Check(ip string) (Verdict, error) {
	allowed, blocked, err := g.rules(ip)
	if err != nil || allowed {
		return Verdict{}, err
	}
	if blocked {
		return Verdict{Blocked: true}, nil
	}
	if !g.settings().Enabled {
		return Verdict{}, nil
	}
	var until int64
	err = g.db.QueryRow(`SELECT until FROM ip_lockouts WHERE ip = ?`, ip).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return Verdict{}, nil
	}
	if err != nil {
		return Verdict{}, fmt.Errorf("guard: %w", err)
	}
	if t := time.UnixMilli(until); t.After(g.now()) {
		return Verdict{Blocked: true, Until: t}, nil
	}
	return Verdict{}, nil
}

// Fail records a failed login from ip (handle as typed, source like
// "telnet" or "web"); it returns the lockout it caused, if any.
func (g *Guard) Fail(ip, handle, source string) (Verdict, error) {
	st := g.settings()
	allowed, _, err := g.rules(ip)
	if err != nil || allowed || !st.Enabled {
		return Verdict{}, err
	}
	now := g.now()
	g.prune(now)
	if _, err := g.db.Exec(`INSERT INTO login_failures (ip, handle, source, at) VALUES (?, ?, ?, ?)`,
		ip, handle, source, now.UnixMilli()); err != nil {
		return Verdict{}, fmt.Errorf("guard: %w", err)
	}
	var n int
	if err := g.db.QueryRow(`SELECT COUNT(*) FROM login_failures WHERE ip = ? AND at > ?`,
		ip, now.Add(-st.Window).UnixMilli()).Scan(&n); err != nil {
		return Verdict{}, fmt.Errorf("guard: %w", err)
	}
	if n < st.MaxFailures {
		return Verdict{}, nil
	}

	// Locked out: longer if it was locked out within the last day.
	strikes := 1
	var prevStrikes int
	var prevUntil int64
	err = g.db.QueryRow(`SELECT strikes, until FROM ip_lockouts WHERE ip = ?`, ip).Scan(&prevStrikes, &prevUntil)
	if err == nil && now.Sub(time.UnixMilli(prevUntil)) < 24*time.Hour {
		strikes = prevStrikes + 1
	}
	d := time.Duration(float64(st.Lockout) * math.Pow(4, float64(strikes-1)))
	if d > st.MaxLockout || d <= 0 {
		d = st.MaxLockout
	}
	until := now.Add(d)
	reason := fmt.Sprintf("%d failed logins in %s (last: %s via %s)", n, st.Window, handle, source)
	if _, err := g.db.Exec(`INSERT INTO ip_lockouts (ip, until, locked_at, strikes, reason) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET until = excluded.until, locked_at = excluded.locked_at,
			strikes = excluded.strikes, reason = excluded.reason`,
		ip, until.UnixMilli(), now.UnixMilli(), strikes, reason); err != nil {
		return Verdict{}, fmt.Errorf("guard: %w", err)
	}
	if _, err := g.db.Exec(`DELETE FROM login_failures WHERE ip = ?`, ip); err != nil {
		return Verdict{}, fmt.Errorf("guard: %w", err)
	}
	if g.log != nil {
		g.log.Warn("locked out %s for %s: %s", ip, d.Round(time.Minute), reason)
	}
	return Verdict{Blocked: true, Until: until}, nil
}

// Succeed clears ip's failures after a good login.
func (g *Guard) Succeed(ip string) error {
	if _, err := g.db.Exec(`DELETE FROM login_failures WHERE ip = ?`, ip); err != nil {
		return fmt.Errorf("guard: %w", err)
	}
	return nil
}

// prune drops old failures and long-expired lockouts, now and then.
func (g *Guard) prune(now time.Time) {
	g.mu.Lock()
	due := now.Sub(g.lastPrune) > 10*time.Minute
	if due {
		g.lastPrune = now
	}
	g.mu.Unlock()
	if !due {
		return
	}
	g.db.Exec(`DELETE FROM login_failures WHERE at < ?`, now.Add(-24*time.Hour).UnixMilli())
	g.db.Exec(`DELETE FROM ip_lockouts WHERE until < ?`, now.Add(-7*24*time.Hour).UnixMilli())
}

// rules says whether ip is on the allow or block list (allow wins).
func (g *Guard) rules(ip string) (allowed, blocked bool, err error) {
	addr, perr := netip.ParseAddr(ip)
	rows, err := g.db.Query(`SELECT pattern, kind FROM ip_rules`)
	if err != nil {
		return false, false, fmt.Errorf("guard: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pattern, kind string
		if err := rows.Scan(&pattern, &kind); err != nil {
			return false, false, fmt.Errorf("guard: %w", err)
		}
		if !matches(pattern, ip, addr, perr == nil) {
			continue
		}
		if kind == "allow" {
			allowed = true
		} else {
			blocked = true
		}
	}
	return allowed, blocked && !allowed, rows.Err()
}

func matches(pattern, ip string, addr netip.Addr, parsed bool) bool {
	if pattern == ip {
		return true
	}
	if !parsed {
		return false
	}
	if p, err := netip.ParsePrefix(pattern); err == nil {
		return p.Contains(addr)
	}
	return false
}

// Lockout is an IP locked out now.
type Lockout struct {
	IP       string    `json:"ip"`
	Until    time.Time `json:"until"`
	LockedAt time.Time `json:"locked_at"`
	Strikes  int       `json:"strikes"`
	Reason   string    `json:"reason"`
}

// Lockouts returns the IPs locked out now, newest first.
func (g *Guard) Lockouts() ([]Lockout, error) {
	rows, err := g.db.Query(`SELECT ip, until, locked_at, strikes, reason FROM ip_lockouts WHERE until > ? ORDER BY locked_at DESC`,
		g.now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("guard: %w", err)
	}
	defer rows.Close()
	out := []Lockout{}
	for rows.Next() {
		var l Lockout
		var until, at int64
		if err := rows.Scan(&l.IP, &until, &at, &l.Strikes, &l.Reason); err != nil {
			return nil, fmt.Errorf("guard: %w", err)
		}
		l.Until, l.LockedAt = time.UnixMilli(until), time.UnixMilli(at)
		out = append(out, l)
	}
	return out, rows.Err()
}

// Unlock ends ip's lockout and forgets its failures and strikes.
func (g *Guard) Unlock(ip string) error {
	if _, err := g.db.Exec(`DELETE FROM ip_lockouts WHERE ip = ?`, ip); err != nil {
		return fmt.Errorf("guard: %w", err)
	}
	return g.Succeed(ip)
}

// Failure is one failed login.
type Failure struct {
	IP     string    `json:"ip"`
	Handle string    `json:"handle"`
	Source string    `json:"source"`
	At     time.Time `json:"at"`
}

// RecentFailures returns the newest failed logins.
func (g *Guard) RecentFailures(limit int) ([]Failure, error) {
	rows, err := g.db.Query(`SELECT ip, handle, source, at FROM login_failures ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("guard: %w", err)
	}
	defer rows.Close()
	out := []Failure{}
	for rows.Next() {
		var f Failure
		var at int64
		if err := rows.Scan(&f.IP, &f.Handle, &f.Source, &at); err != nil {
			return nil, fmt.Errorf("guard: %w", err)
		}
		f.At = time.UnixMilli(at)
		out = append(out, f)
	}
	return out, rows.Err()
}

// Rule is an allow or block list entry.
type Rule struct {
	Pattern   string    `json:"pattern"`
	Kind      string    `json:"kind"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// Rules returns the allow and block lists.
func (g *Guard) Rules() ([]Rule, error) {
	rows, err := g.db.Query(`SELECT pattern, kind, note, created_at FROM ip_rules ORDER BY kind, pattern`)
	if err != nil {
		return nil, fmt.Errorf("guard: %w", err)
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		var at int64
		if err := rows.Scan(&r.Pattern, &r.Kind, &r.Note, &at); err != nil {
			return nil, fmt.Errorf("guard: %w", err)
		}
		r.CreatedAt = time.UnixMilli(at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrBadPattern is a rule that's neither an IP nor a CIDR range.
var ErrBadPattern = errors.New("guard: not an IP address or CIDR range")

// AddRule adds (or changes) an allow or block list entry; pattern is
// normalized ("203.0.113.0/24", "2001:db8::1").
func (g *Guard) AddRule(pattern, kind, note string) (string, error) {
	if kind != "allow" && kind != "block" {
		return "", fmt.Errorf("guard: kind must be allow or block")
	}
	pattern = strings.TrimSpace(pattern)
	if a, err := netip.ParseAddr(pattern); err == nil {
		pattern = a.Unmap().String()
	} else if p, err := netip.ParsePrefix(pattern); err == nil {
		pattern = p.Masked().String()
	} else {
		return "", ErrBadPattern
	}
	_, err := g.db.Exec(`INSERT INTO ip_rules (pattern, kind, note, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(pattern) DO UPDATE SET kind = excluded.kind, note = excluded.note`,
		pattern, kind, strings.TrimSpace(note), g.now().UnixMilli())
	if err != nil {
		return "", fmt.Errorf("guard: %w", err)
	}
	if kind == "allow" {
		// Allowed now: not locked out either.
		if a, err := netip.ParseAddr(pattern); err == nil {
			g.Unlock(a.String())
		}
	}
	return pattern, nil
}

// DeleteRule removes an allow or block list entry.
func (g *Guard) DeleteRule(pattern string) error {
	if _, err := g.db.Exec(`DELETE FROM ip_rules WHERE pattern = ?`, pattern); err != nil {
		return fmt.Errorf("guard: %w", err)
	}
	return nil
}

// Conns counts a daemon's open connections per IP, for the limit on
// simultaneous Telnet/SSH connections.
type Conns struct {
	mu sync.Mutex
	n  map[string]int
}

// Open counts a new connection from ip unless that would exceed max
// (0: no limit); ok false means refused, and nothing is counted.
func (c *Conns) Open(ip string, max int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	if max > 0 && c.n[ip] >= max {
		return false
	}
	c.n[ip]++
	return true
}

// Close counts a connection from ip as closed.
func (c *Conns) Close(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[ip] <= 1 {
		delete(c.n, ip)
	} else {
		c.n[ip]--
	}
}
