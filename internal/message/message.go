// Package message implements the BBS's message base: named areas
// (boards), each SL-gated for reading and posting, holding the
// messages callers post to them.
package message

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTagTaken is returned by CreateArea when the tag is already in
// use (case-insensitively).
var ErrTagTaken = errors.New("message: area tag already taken")

// ErrAreaNotFound is returned when an area tag or ID doesn't exist.
var ErrAreaNotFound = errors.New("message: area not found")

// Area is one named message board.
type Area struct {
	ID          int64
	Tag         string
	Name        string
	Description string
	MinSLRead   int
	MinSLWrite  int
	SortOrder   int
	CreatedAt   time.Time
}

// CanRead reports whether an account at securityLevel may read this
// area's messages.
func (a Area) CanRead(securityLevel int) bool { return securityLevel >= a.MinSLRead }

// CanWrite reports whether an account at securityLevel may post to
// this area.
func (a Area) CanWrite(securityLevel int) bool { return securityLevel >= a.MinSLWrite }

// Message is one post within an Area.
type Message struct {
	ID         int64
	AreaID     int64
	FromUserID int64
	FromName   string // joined from users.username for display
	ToName     string
	Subject    string
	Body       string
	PostedAt   time.Time
}

// Store persists Areas and Messages in the shared SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore wraps an already-opened database handle (see internal/db).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// CreateArea adds a new message area.
func (s *Store) CreateArea(tag, name, description string, minSLRead, minSLWrite int) (*Area, error) {
	res, err := s.db.Exec(
		`INSERT INTO message_areas (tag, name, description, min_sl_read, min_sl_write) VALUES (?, ?, ?, ?, ?)`,
		tag, name, description, minSLRead, minSLWrite,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil, ErrTagTaken
		}
		return nil, fmt.Errorf("message: create area %s: %w", tag, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("message: last insert id: %w", err)
	}
	return s.AreaByID(id)
}

// AreaByID loads a single area by primary key.
func (s *Store) AreaByID(id int64) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, min_sl_read, min_sl_write, sort_order, created_at
		 FROM message_areas WHERE id = ?`, id,
	))
}

// AreaByTag loads a single area by its short tag (case-insensitive).
func (s *Store) AreaByTag(tag string) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, min_sl_read, min_sl_write, sort_order, created_at
		 FROM message_areas WHERE tag = ?`, tag,
	))
}

func (s *Store) scanArea(row *sql.Row) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAreaNotFound
		}
		return nil, fmt.Errorf("message: load area: %w", err)
	}
	return &a, nil
}

// ListAreas returns every area readable at securityLevel, ordered for
// menu display.
func (s *Store) ListAreas(securityLevel int) ([]Area, error) {
	rows, err := s.db.Query(
		`SELECT id, tag, name, description, min_sl_read, min_sl_write, sort_order, created_at
		 FROM message_areas WHERE min_sl_read <= ? ORDER BY sort_order, name`, securityLevel,
	)
	if err != nil {
		return nil, fmt.Errorf("message: list areas: %w", err)
	}
	defer rows.Close()

	var areas []Area
	for rows.Next() {
		var a Area
		if err := rows.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("message: scan area: %w", err)
		}
		areas = append(areas, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: list areas: %w", err)
	}
	return areas, nil
}

// PostMessage adds a new message to an area, posted by fromUserID.
func (s *Store) PostMessage(areaID, fromUserID int64, toName, subject, body string) (*Message, error) {
	res, err := s.db.Exec(
		`INSERT INTO messages (area_id, from_user_id, to_name, subject, body) VALUES (?, ?, ?, ?, ?)`,
		areaID, fromUserID, toName, subject, body,
	)
	if err != nil {
		return nil, fmt.Errorf("message: post to area %d: %w", areaID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("message: last insert id: %w", err)
	}
	return s.MessageByID(id)
}

// MessageByID loads a single message, with its author's current
// username joined in as FromName.
func (s *Store) MessageByID(id int64) (*Message, error) {
	row := s.db.QueryRow(
		`SELECT m.id, m.area_id, m.from_user_id, u.username, m.to_name, m.subject, m.body, m.posted_at
		 FROM messages m JOIN users u ON u.id = m.from_user_id WHERE m.id = ?`, id,
	)
	var m Message
	if err := row.Scan(&m.ID, &m.AreaID, &m.FromUserID, &m.FromName, &m.ToName, &m.Subject, &m.Body, &m.PostedAt); err != nil {
		return nil, fmt.Errorf("message: load %d: %w", id, err)
	}
	return &m, nil
}

// ListMessages returns every message in an area, oldest first, with
// each author's current username joined in as FromName.
func (s *Store) ListMessages(areaID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.area_id, m.from_user_id, u.username, m.to_name, m.subject, m.body, m.posted_at
		 FROM messages m JOIN users u ON u.id = m.from_user_id
		 WHERE m.area_id = ? ORDER BY m.posted_at, m.id`, areaID,
	)
	if err != nil {
		return nil, fmt.Errorf("message: list messages for area %d: %w", areaID, err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.AreaID, &m.FromUserID, &m.FromName, &m.ToName, &m.Subject, &m.Body, &m.PostedAt); err != nil {
			return nil, fmt.Errorf("message: scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: list messages for area %d: %w", areaID, err)
	}
	return messages, nil
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
