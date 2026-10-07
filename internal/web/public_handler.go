package web

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/midrei/nullmodem-kit/ansi"

	"github.com/midrei/nullmodem-bbs/internal/version"
)

// The public front page (/): everything a visitor -- or a BBS list --
// wants to know about this board without logging in: how to connect,
// who's on, who called lately, what's here. Only what a classic BBS
// shows its callers anyway (handles, not addresses or real names).

type publicNetworkDTO struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
}

type publicCallerDTO struct {
	Handle string    `json:"handle"`
	At     time.Time `json:"at"`
	Place  string    `json:"place,omitempty"`
}

type publicOnlineDTO struct {
	Node   int       `json:"node"`
	Handle string    `json:"handle"`
	Since  time.Time `json:"since"`
}

type publicOverviewDTO struct {
	Name       string             `json:"name"`
	Sysop      string             `json:"sysop"`
	Location   string             `json:"location"`
	Version    string             `json:"version"`
	Build      string             `json:"build,omitempty"`
	TelnetPort string             `json:"telnet_port,omitempty"`
	SSHPort    string             `json:"ssh_port,omitempty"`
	BinkpPort  string             `json:"binkp_port,omitempty"`
	Networks   []publicNetworkDTO `json:"networks"`
	Online     []publicOnlineDTO  `json:"online"`
	Callers    []publicCallerDTO  `json:"callers"`
	Oneliners  []map[string]any   `json:"oneliners"`
	Doors      []string           `json:"doors"`
	Stats      map[string]int     `json:"stats"`
	// SinceYear: the year of the first account, 0 if unknown.
	SinceYear int `json:"since_year,omitempty"`
}

var (
	publicMu     sync.Mutex
	publicCached *publicOverviewDTO
	publicAt     time.Time
)

// handlePublicOverview: GET /api/public/overview (no login), cached
// for half a minute.
func (s *Server) handlePublicOverview(w http.ResponseWriter, r *http.Request) {
	publicMu.Lock()
	defer publicMu.Unlock()
	if publicCached == nil || time.Since(publicAt) > 30*time.Second {
		o, err := s.publicOverview()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load the overview")
			return
		}
		publicCached, publicAt = o, time.Now()
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, http.StatusOK, publicCached)
}

func (s *Server) publicOverview() (*publicOverviewDTO, error) {
	c, err := s.loadBBSConfig()
	if err != nil {
		return nil, err
	}
	o := &publicOverviewDTO{
		Name: c.BBS.Name, Sysop: c.BBS.Sysop, Location: c.BBS.Location, Version: version.Short(), Build: version.Build(),
		Networks: []publicNetworkDTO{}, Online: []publicOnlineDTO{}, Callers: []publicCallerDTO{},
		Oneliners: []map[string]any{}, Doors: []string{}, Stats: map[string]int{},
	}
	if c.Telnet.Enabled {
		o.TelnetPort = portOf(c.Telnet.Addr)
	}
	if c.SSH.Enabled {
		o.SSHPort = portOf(c.SSH.Addr)
	}
	if c.Binkp.ListenEnabled && len(c.BBS.FTNAddresses) > 0 {
		o.BinkpPort = portOf(c.Binkp.ListenAddr)
	}
	// This board's addresses, by network (their @domain).
	for _, n := range c.Networks {
		var addrs []string
		for _, a := range c.BBS.FTNAddresses {
			if at := strings.LastIndex(a, "@"); at > 0 && strings.EqualFold(a[at+1:], n.Domain) {
				addrs = append(addrs, a[:at])
			}
		}
		if len(addrs) > 0 {
			o.Networks = append(o.Networks, publicNetworkDTO{Name: n.Name, Addresses: addrs})
		}
	}
	for _, d := range c.Doors {
		o.Doors = append(o.Doors, d.Name)
	}

	if nodes, err := s.Nodes.List(); err == nil {
		for _, n := range nodes {
			handle := n.Username
			if handle == "" {
				handle = "(logging in)"
			}
			o.Online = append(o.Online, publicOnlineDTO{Node: n.Node, Handle: handle, Since: n.ConnectedAt})
		}
	}
	rows, err := s.DB.Query(`SELECT username, last_login_at, location FROM users
		WHERE last_login_at IS NOT NULL AND validated = 1 ORDER BY last_login_at DESC LIMIT 8`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c publicCallerDTO
		rows.Scan(&c.Handle, &c.At, &c.Place)
		o.Callers = append(o.Callers, c)
	}
	rows.Close()
	if s.Chat != nil {
		if list, err := s.Chat.Oneliners(5); err == nil {
			for _, l := range list {
				o.Oneliners = append(o.Oneliners, map[string]any{"handle": l.Username, "text": l.Text, "at": l.At})
			}
		}
	}

	count := func(key, q string) {
		var n int
		if err := s.DB.QueryRow(q).Scan(&n); err == nil {
			o.Stats[key] = n
		}
	}
	count("users", `SELECT COUNT(*) FROM users WHERE validated = 1`)
	count("calls", `SELECT COALESCE(SUM(total_calls), 0) FROM users`)
	count("messages", `SELECT COUNT(*) FROM messages m JOIN message_areas a ON a.id = m.area_id WHERE a.hidden = 0 AND a.pending = 0`)
	count("message_areas", `SELECT COUNT(*) FROM message_areas WHERE hidden = 0 AND pending = 0`)
	count("files", `SELECT COUNT(*) FROM files`)
	count("file_areas", `SELECT COUNT(*) FROM file_areas WHERE pending = 0`)
	o.Stats["doors"] = len(o.Doors)
	o.Stats["networks"] = len(o.Networks)
	// MIN() of a TIMESTAMP column comes back as text, not a time.
	var first sql.NullString
	if err := s.DB.QueryRow(`SELECT MIN(created_at) FROM users`).Scan(&first); err == nil && len(first.String) >= 4 {
		o.SinceYear, _ = strconv.Atoi(first.String[:4])
	}
	// Names are CP437 when they came over Telnet; the page is UTF-8.
	for i := range o.Callers {
		o.Callers[i].Place = ansi.DecodeCP437([]byte(o.Callers[i].Place))
	}
	return o, nil
}
