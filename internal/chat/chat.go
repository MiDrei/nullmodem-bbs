// Package chat is the BBS's live talk between people: chat rooms (the
// teleconference everyone may enter, and a room per caller paging the
// sysop) and the one-liners wall. Rooms live in the shared database,
// so a Telnet/SSH caller (bbs daemon) and the sysop in the web admin
// (web daemon) talk in the same room; each side polls for new lines.
package chat

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/midrei/nullmodem-bbs/internal/textclean"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Main is the teleconference everyone may enter.
const Main = "main"

// PagePrefix starts a paging caller's room: "page-<handle>".
const PagePrefix = "page-"

// PageRoom is the room a caller paging the sysop waits in.
func PageRoom(handle string) string {
	return PagePrefix + strings.ToLower(handle)
}

var roomRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

// ValidRoom reports whether name may be a room.
func ValidRoom(name string) bool { return roomRE.MatchString(name) }

// SourceDiscord marks a line said on Discord (internal/discord).
const SourceDiscord = "discord"

// Kinds of line.
const (
	Say   = "say"
	Join  = "join"
	Leave = "leave"
	Page  = "page"
)

// PresentFor is how long someone counts as in a room after their last
// sign of life (Touch).
const PresentFor = 15 * time.Second

// MaxText is the longest line.
const MaxText = 400

// Line is one line in a room.
type Line struct {
	ID       int64     `json:"id"`
	Room     string    `json:"room"`
	Username string    `json:"username"`
	Source   string    `json:"source"`
	Kind     string    `json:"kind"`
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
}

// Store keeps the rooms and the one-liners.
type Store struct {
	db  *sql.DB
	now func() time.Time

	mu        sync.Mutex
	lastPrune time.Time

	// Quiet, if set, names who enters and leaves rooms without a word
	// (the sysops, unless the board wants them announced).
	Quiet func(username string) bool
}

func NewStore(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

// ErrBadRoom is a room name ValidRoom refuses.
var ErrBadRoom = errors.New("chat: not a room name")

// Post adds a line to room.
func (s *Store) Post(room, username, source, kind, text string) (Line, error) {
	username, text = textclean.Line(username), textclean.Line(text)
	if !ValidRoom(room) {
		return Line{}, ErrBadRoom
	}
	text = strings.TrimSpace(text)
	if len(text) > MaxText {
		text = text[:MaxText]
	}
	now := s.now()
	s.prune(now)
	res, err := s.db.Exec(`INSERT INTO chat_lines (room, username, source, kind, text, at) VALUES (?, ?, ?, ?, ?, ?)`,
		room, username, source, kind, text, now.UnixMilli())
	if err != nil {
		return Line{}, fmt.Errorf("chat: %w", err)
	}
	id, _ := res.LastInsertId()
	return Line{ID: id, Room: room, Username: username, Source: source, Kind: kind, Text: text, At: now}, nil
}

// Lines returns room's lines after afterID, oldest first -- or, with
// afterID 0, its newest limit.
func (s *Store) Lines(room string, afterID int64, limit int) ([]Line, error) {
	var rows *sql.Rows
	var err error
	if afterID > 0 {
		rows, err = s.db.Query(`SELECT id, room, username, source, kind, text, at FROM chat_lines
			WHERE room = ? AND id > ? ORDER BY id LIMIT ?`, room, afterID, limit)
	} else {
		rows, err = s.db.Query(`SELECT * FROM (SELECT id, room, username, source, kind, text, at FROM chat_lines
			WHERE room = ? ORDER BY id DESC LIMIT ?) ORDER BY id`, room, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	defer rows.Close()
	return scanLines(rows)
}

func scanLines(rows *sql.Rows) ([]Line, error) {
	out := []Line{}
	for rows.Next() {
		var l Line
		var at int64
		if err := rows.Scan(&l.ID, &l.Room, &l.Username, &l.Source, &l.Kind, &l.Text, &at); err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}
		l.At = time.UnixMilli(at)
		out = append(out, l)
	}
	return out, rows.Err()
}

// Enter puts username (on source: "node 2", "web") into room and says
// so; Touch keeps them there, Exit takes them out.
func (s *Store) Enter(room, username, source string) error {
	if err := s.Touch(room, username, source); err != nil {
		return err
	}
	if s.quiet(username) {
		return nil
	}
	_, err := s.Post(room, username, source, Join, "")
	return err
}

func (s *Store) quiet(username string) bool {
	return s.Quiet != nil && s.Quiet(username)
}

// Touch marks username as still in room.
func (s *Store) Touch(room, username, source string) error {
	if !ValidRoom(room) {
		return ErrBadRoom
	}
	_, err := s.db.Exec(`INSERT INTO chat_presence (room, username, source, last_seen) VALUES (?, ?, ?, ?)
		ON CONFLICT(room, username, source) DO UPDATE SET last_seen = excluded.last_seen`,
		room, username, source, s.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}

// Exit takes username out of room and says so.
func (s *Store) Exit(room, username, source string) error {
	if _, err := s.db.Exec(`DELETE FROM chat_presence WHERE room = ? AND username = ? AND source = ?`, room, username, source); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	if s.quiet(username) {
		return nil
	}
	_, err := s.Post(room, username, source, Leave, "")
	return err
}

// Clear deletes everything said in room; who's in it stays.
func (s *Store) Clear(room string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM chat_lines WHERE room = ?`, room)
	if err != nil {
		return 0, fmt.Errorf("chat: %w", err)
	}
	return res.RowsAffected()
}

// Presence is someone in a room.
type Presence struct {
	Username string `json:"username"`
	Source   string `json:"source"`
}

// Present returns who is in room now.
func (s *Store) Present(room string) ([]Presence, error) {
	rows, err := s.db.Query(`SELECT username, source FROM chat_presence WHERE room = ? AND last_seen > ? ORDER BY username`,
		room, s.now().Add(-PresentFor).UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	defer rows.Close()
	out := []Presence{}
	for rows.Next() {
		var p Presence
		if err := rows.Scan(&p.Username, &p.Source); err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Room is a room with something going on: someone in it, or a line in
// the last day.
type Room struct {
	Name     string     `json:"name"`
	Present  []Presence `json:"present"`
	LastLine *Line      `json:"last_line"`
	// Paging is set for a caller's page room with that caller in it:
	// someone waiting for the sysop.
	Paging bool `json:"paging"`
}

// Rooms returns the rooms callers may enter and any other with
// something going on, page rooms with someone waiting first.
func (s *Store) Rooms() ([]Room, error) {
	since := s.now().Add(-24 * time.Hour).UnixMilli()
	rows, err := s.db.Query(`SELECT DISTINCT room FROM chat_lines WHERE at > ?
		UNION SELECT DISTINCT room FROM chat_presence WHERE last_seen > ?`, since, s.now().Add(-PresentFor).UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	names := map[string]bool{Main: true}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names[n] = true
	}
	rows.Close()
	// And every room callers may enter, quiet or not.
	if listed, err := s.ListRooms(); err == nil {
		for _, r := range listed {
			names[r.Name] = true
		}
	}
	var out []Room
	for n := range names {
		r := Room{Name: n}
		if r.Present, err = s.Present(n); err != nil {
			return nil, err
		}
		if lines, err := s.Lines(n, 0, 1); err == nil && len(lines) > 0 {
			r.LastLine = &lines[0]
		}
		if strings.HasPrefix(n, PagePrefix) {
			who := strings.TrimPrefix(n, PagePrefix)
			for _, p := range r.Present {
				if strings.EqualFold(p.Username, who) {
					r.Paging = true
				}
			}
		}
		out = append(out, r)
	}
	// Waiting callers first, then the main room, then by last line.
	rank := func(r Room) int {
		switch {
		case r.Paging:
			return 0
		case r.Name == Main:
			return 1
		}
		return 2
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			later := func(x Room) int64 {
				if x.LastLine == nil {
					return 0
				}
				return x.LastLine.ID
			}
			if rank(b) < rank(a) || (rank(b) == rank(a) && later(b) > later(a)) {
				out[j-1], out[j] = b, a
			}
		}
	}
	return out, nil
}

// prune drops lines older than 30 days and stale presence, now and then.
func (s *Store) prune(now time.Time) {
	s.mu.Lock()
	due := now.Sub(s.lastPrune) > time.Hour
	if due {
		s.lastPrune = now
	}
	s.mu.Unlock()
	if !due {
		return
	}
	s.db.Exec(`DELETE FROM chat_lines WHERE at < ?`, now.Add(-30*24*time.Hour).UnixMilli())
	s.db.Exec(`DELETE FROM chat_presence WHERE last_seen < ?`, now.Add(-time.Hour).UnixMilli())
}

// Oneliner is a line on the wall.
type Oneliner struct {
	ID       int64     `json:"id"`
	Username string    `json:"username"`
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
}

// MaxOneliner is the longest one-liner -- it has to fit a row beside
// its writer's name.
const MaxOneliner = 60

// AddOneliner puts a line on the wall.
func (s *Store) AddOneliner(userID int64, username, text string) (Oneliner, error) {
	text = strings.TrimSpace(textclean.Line(text))
	if text == "" {
		return Oneliner{}, errors.New("chat: empty one-liner")
	}
	if len(text) > MaxOneliner {
		text = text[:MaxOneliner]
	}
	now := s.now()
	res, err := s.db.Exec(`INSERT INTO oneliners (user_id, username, text, at) VALUES (?, ?, ?, ?)`, userID, username, text, now.UnixMilli())
	if err != nil {
		return Oneliner{}, fmt.Errorf("chat: %w", err)
	}
	id, _ := res.LastInsertId()
	return Oneliner{ID: id, Username: username, Text: text, At: now}, nil
}

// Oneliners returns the newest limit lines on the wall, oldest first.
func (s *Store) Oneliners(limit int) ([]Oneliner, error) {
	rows, err := s.db.Query(`SELECT * FROM (SELECT id, username, text, at FROM oneliners ORDER BY id DESC LIMIT ?) ORDER BY id`, limit)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	defer rows.Close()
	out := []Oneliner{}
	for rows.Next() {
		var o Oneliner
		var at int64
		if err := rows.Scan(&o.ID, &o.Username, &o.Text, &at); err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}
		o.At = time.UnixMilli(at)
		out = append(out, o)
	}
	return out, rows.Err()
}

// DeleteOneliner takes a line off the wall.
func (s *Store) DeleteOneliner(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM oneliners WHERE id = ?`, id); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}
