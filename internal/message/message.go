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
	// Network groups related echo areas by FTN network (e.g.
	// "fsxNet", "FidoNet") once a BinkP mailer exists to feed them;
	// empty means a local-only area with no network affiliation.
	Network    string
	MinSLRead  int
	MinSLWrite int
	SortOrder  int
	CreatedAt  time.Time
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

// CreateArea adds a new message area. network is the FTN network it
// belongs to (e.g. "fsxNet"), or "" for a local-only area.
func (s *Store) CreateArea(tag, name, description, network string, minSLRead, minSLWrite int) (*Area, error) {
	res, err := s.db.Exec(
		`INSERT INTO message_areas (tag, name, description, network, min_sl_read, min_sl_write) VALUES (?, ?, ?, ?, ?, ?)`,
		tag, name, description, network, minSLRead, minSLWrite,
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
		`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at
		 FROM message_areas WHERE id = ?`, id,
	))
}

// AreaByTag loads a single area by its short tag (case-insensitive).
func (s *Store) AreaByTag(tag string) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at
		 FROM message_areas WHERE tag = ?`, tag,
	))
}

func (s *Store) scanArea(row *sql.Row) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAreaNotFound
		}
		return nil, fmt.Errorf("message: load area: %w", err)
	}
	return &a, nil
}

// CountAreas returns the total number of message areas, for the web
// admin dashboard.
func (s *Store) CountAreas() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM message_areas`).Scan(&n); err != nil {
		return 0, fmt.Errorf("message: count areas: %w", err)
	}
	return n, nil
}

// ListAreas returns every area readable at securityLevel, grouped by
// network (local/ungrouped areas -- empty Network -- sort first) then
// ordered for menu display within each group.
func (s *Store) ListAreas(securityLevel int) ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at
		 FROM message_areas WHERE min_sl_read <= ? ORDER BY network, sort_order, name`, securityLevel)
}

// AllAreas returns every area regardless of SL gating, for the web
// admin area management UI, in the same network-grouped order as
// ListAreas.
func (s *Store) AllAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at
		 FROM message_areas ORDER BY network, sort_order, name`)
}

func (s *Store) queryAreas(query string, args ...any) ([]Area, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("message: list areas: %w", err)
	}
	defer rows.Close()

	var areas []Area
	for rows.Next() {
		var a Area
		if err := rows.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("message: scan area: %w", err)
		}
		areas = append(areas, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: list areas: %w", err)
	}
	return areas, nil
}

// AreaWithStats bundles an Area with one caller's per-area message
// counts, for the lightbar area listing: Total messages, New (posted
// after that caller's last visit, or all of them if they've never
// visited), and Yours (their own posts in the area).
type AreaWithStats struct {
	Area  Area
	Total int
	New   int
	Yours int
}

// ListAreaStats is ListAreas plus, for userID, each area's Total/New/
// Yours counts (see AreaWithStats) in a single query.
func (s *Store) ListAreaStats(securityLevel int, userID int64) ([]AreaWithStats, error) {
	rows, err := s.db.Query(
		`SELECT a.id, a.tag, a.name, a.description, a.network, a.min_sl_read, a.min_sl_write, a.sort_order, a.created_at,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id) AS total,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id AND m.from_user_id = ?) AS yours,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id
		           AND NOT EXISTS (SELECT 1 FROM message_reads r
		                           WHERE r.user_id = ? AND r.message_id = m.id)) AS new
		 FROM message_areas a
		 WHERE a.min_sl_read <= ?
		 ORDER BY a.network, a.sort_order, a.name`,
		userID, userID, securityLevel,
	)
	if err != nil {
		return nil, fmt.Errorf("message: list area stats: %w", err)
	}
	defer rows.Close()

	var stats []AreaWithStats
	for rows.Next() {
		var st AreaWithStats
		if err := rows.Scan(&st.Area.ID, &st.Area.Tag, &st.Area.Name, &st.Area.Description, &st.Area.Network,
			&st.Area.MinSLRead, &st.Area.MinSLWrite, &st.Area.SortOrder, &st.Area.CreatedAt,
			&st.Total, &st.Yours, &st.New); err != nil {
			return nil, fmt.Errorf("message: scan area stats: %w", err)
		}
		stats = append(stats, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: list area stats: %w", err)
	}
	return stats, nil
}

// ReadMessageIDs returns the set of message IDs within areaID that
// userID has actually opened in the reader, so a caller can flag each
// row in the message list as new (its ID is absent from this set)
// without touching the read state itself -- unlike the old area-level
// watermark, entering the area/list no longer marks anything read.
func (s *Store) ReadMessageIDs(userID, areaID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(
		`SELECT r.message_id FROM message_reads r
		 JOIN messages m ON m.id = r.message_id
		 WHERE r.user_id = ? AND m.area_id = ?`,
		userID, areaID,
	)
	if err != nil {
		return nil, fmt.Errorf("message: read message ids for area %d, user %d: %w", areaID, userID, err)
	}
	defer rows.Close()

	read := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("message: scan read message id: %w", err)
		}
		read[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: read message ids for area %d, user %d: %w", areaID, userID, err)
	}
	return read, nil
}

// MarkMessageRead records that userID has actually opened messageID
// in the reader, so ListAreaStats/ReadMessageIDs stop counting it as
// new. Idempotent: reading the same message again is a no-op.
func (s *Store) MarkMessageRead(userID, messageID int64) error {
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO message_reads (user_id, message_id) VALUES (?, ?)`,
		userID, messageID,
	); err != nil {
		return fmt.Errorf("message: mark message %d read for user %d: %w", messageID, userID, err)
	}
	return nil
}

// UpdateArea changes an existing area's editable fields (not its tag,
// which is treated as a stable identifier once created).
func (s *Store) UpdateArea(id int64, name, description, network string, minSLRead, minSLWrite, sortOrder int) (*Area, error) {
	if _, err := s.db.Exec(
		`UPDATE message_areas SET name = ?, description = ?, network = ?, min_sl_read = ?, min_sl_write = ?, sort_order = ? WHERE id = ?`,
		name, description, network, minSLRead, minSLWrite, sortOrder, id,
	); err != nil {
		return nil, fmt.Errorf("message: update area %d: %w", id, err)
	}
	return s.AreaByID(id)
}

// DeleteArea removes an area and, via ON DELETE CASCADE, every
// message posted in it.
func (s *Store) DeleteArea(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM message_areas WHERE id = ?`, id); err != nil {
		return fmt.Errorf("message: delete area %d: %w", id, err)
	}
	return nil
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
