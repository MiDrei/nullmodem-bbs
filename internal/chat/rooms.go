package chat

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// RoomInfo is a room callers may enter: the teleconference (Main) and
// whatever the sysop adds. DiscordChannel, if set, is the Discord
// channel it's bridged to (internal/discord).
type RoomInfo struct {
	Name           string `json:"name"`
	Title          string `json:"title"`
	Topic          string `json:"topic"`
	MinSL          int    `json:"min_sl"`
	SortOrder      int    `json:"sort_order"`
	DiscordChannel string `json:"discord_channel"`
	// MatrixRoom, if set, is the Matrix room ID it's bridged to.
	MatrixRoom string `json:"matrix_room"`
}

// ErrNoRoom: no such room.
var ErrNoRoom = errors.New("chat: no such room")

// ErrMainRoom: the teleconference can't be removed.
var ErrMainRoom = errors.New("chat: the teleconference stays")

// ListRooms returns the rooms, the teleconference first.
func (s *Store) ListRooms() ([]RoomInfo, error) {
	rows, err := s.db.Query(`SELECT name, title, topic, min_sl, sort_order, discord_channel, matrix_room FROM chat_rooms
		ORDER BY name != 'main', sort_order, name`)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	defer rows.Close()
	out := []RoomInfo{}
	for rows.Next() {
		var r RoomInfo
		if err := rows.Scan(&r.Name, &r.Title, &r.Topic, &r.MinSL, &r.SortOrder, &r.DiscordChannel, &r.MatrixRoom); err != nil {
			return nil, fmt.Errorf("chat: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RoomsFor returns the rooms someone with securityLevel may enter.
func (s *Store) RoomsFor(securityLevel int) ([]RoomInfo, error) {
	all, err := s.ListRooms()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, r := range all {
		if securityLevel >= r.MinSL {
			out = append(out, r)
		}
	}
	return out, nil
}

// RoomByName returns a listed room.
func (s *Store) RoomByName(name string) (*RoomInfo, error) {
	var r RoomInfo
	err := s.db.QueryRow(`SELECT name, title, topic, min_sl, sort_order, discord_channel, matrix_room FROM chat_rooms WHERE name = ?`,
		strings.ToLower(name)).Scan(&r.Name, &r.Title, &r.Topic, &r.MinSL, &r.SortOrder, &r.DiscordChannel, &r.MatrixRoom)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoRoom
	}
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	return &r, nil
}

// SaveRoom adds or updates r (by name).
func (s *Store) SaveRoom(r RoomInfo) error {
	r.Name = strings.ToLower(strings.TrimSpace(r.Name))
	if !ValidRoom(r.Name) || strings.HasPrefix(r.Name, PagePrefix) {
		return ErrBadRoom
	}
	r.Title = strings.TrimSpace(r.Title)
	if r.Title == "" {
		r.Title = r.Name
	}
	if _, err := s.db.Exec(`INSERT INTO chat_rooms (name, title, topic, min_sl, sort_order, discord_channel, matrix_room) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET title = excluded.title, topic = excluded.topic, min_sl = excluded.min_sl,
			sort_order = excluded.sort_order, discord_channel = excluded.discord_channel, matrix_room = excluded.matrix_room`,
		r.Name, r.Title, strings.TrimSpace(r.Topic), r.MinSL, r.SortOrder, strings.TrimSpace(r.DiscordChannel), strings.TrimSpace(r.MatrixRoom)); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}

// DeleteRoom removes a room (not the teleconference); its lines expire.
func (s *Store) DeleteRoom(name string) error {
	if name == Main {
		return ErrMainRoom
	}
	res, err := s.db.Exec(`DELETE FROM chat_rooms WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoRoom
	}
	return nil
}

// LinesSince returns lines of every room after afterID, oldest first,
// at most limit -- what a bridge forwards.
func (s *Store) LinesSince(afterID int64, limit int) ([]Line, error) {
	rows, err := s.db.Query(`SELECT id, room, username, source, kind, text, at FROM chat_lines WHERE id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}
	defer rows.Close()
	return scanLines(rows)
}

// LastLineID is the newest line's id (0 if none).
func (s *Store) LastLineID() (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(id) FROM chat_lines`).Scan(&id); err != nil {
		return 0, fmt.Errorf("chat: %w", err)
	}
	return id.Int64, nil
}
