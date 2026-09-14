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
	// Pending is set on an area internal/tosser auto-created for an
	// inbound echomail AREA kludge it hadn't seen before (see
	// EnsureArea). A pending area is invisible to every BBS-facing
	// listing and the normal web admin area list until the sysop
	// reviews and approves it (ApproveArea) -- an area created by
	// hand (CreateArea) is never pending.
	Pending bool
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
	FromUserID sql.NullInt64
	FromName   string // joined from users.username for a local poster, or the stored name for a remote one (see ReceiveEcho)
	ToName     string
	Subject    string
	Body       string
	PostedAt   time.Time
}

// IsFromRemote reports whether m arrived from a remote FTN system via
// internal/tosser's echomail toss rather than being posted locally.
func (m *Message) IsFromRemote() bool { return !m.FromUserID.Valid }

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
		`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending
		 FROM message_areas WHERE id = ?`, id,
	))
}

// AreaByTag loads a single area by its short tag (case-insensitive).
func (s *Store) AreaByTag(tag string) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending
		 FROM message_areas WHERE tag = ?`, tag,
	))
}

func (s *Store) scanArea(row *sql.Row) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt, &a.Pending); err != nil {
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

// Networks returns every distinct non-empty Network value already in
// use across all areas (pending or not), sorted -- the choices the
// web admin offers when grouping an area, so a sysop reuses an
// existing group name instead of accidentally typo-ing a near-miss.
func (s *Store) Networks() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT network FROM message_areas WHERE network != '' ORDER BY network`)
	if err != nil {
		return nil, fmt.Errorf("message: list networks: %w", err)
	}
	defer rows.Close()

	var networks []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("message: scan network: %w", err)
		}
		networks = append(networks, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: list networks: %w", err)
	}
	return networks, nil
}

// ListAreas returns every non-pending area readable at securityLevel,
// grouped by network (local/ungrouped areas -- empty Network -- sort
// first) then ordered for menu display within each group. A pending
// area (see EnsureArea) never appears here, however low
// securityLevel's threshold -- it's invisible until approved.
func (s *Store) ListAreas(securityLevel int) ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending
		 FROM message_areas WHERE min_sl_read <= ? AND pending = 0 ORDER BY network, sort_order, name`, securityLevel)
}

// AllAreas returns every non-pending area regardless of SL gating,
// for the web admin area management UI, in the same network-grouped
// order as ListAreas. See PendingAreas for the areas this excludes.
func (s *Store) AllAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending
		 FROM message_areas WHERE pending = 0 ORDER BY network, sort_order, name`)
}

// PendingAreas returns every area awaiting sysop review (see
// EnsureArea), oldest first -- what the web admin's Pending Areas page
// lists for approval.
func (s *Store) PendingAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending
		 FROM message_areas WHERE pending = 1 ORDER BY created_at`)
}

// ApproveArea clears an area's Pending flag, making it visible in
// ListAreas/ListAreaStats and the normal admin area list. Idempotent:
// approving an already-approved area is a no-op.
func (s *Store) ApproveArea(id int64) error {
	if _, err := s.db.Exec(`UPDATE message_areas SET pending = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("message: approve area %d: %w", id, err)
	}
	return nil
}

// EnsureArea returns the area tagged tag, creating it as Pending if it
// doesn't exist yet -- what internal/tosser calls for an inbound
// echomail AREA kludge it hasn't seen before. name and network seed
// the new area's display name and network grouping (both editable
// later when the sysop reviews it); an already-existing area
// (pending or not) is returned unchanged, so a tosser never
// resurrects or reconfigures an area the sysop has already touched.
func (s *Store) EnsureArea(tag, name, network string) (area *Area, created bool, err error) {
	if existing, err := s.AreaByTag(tag); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrAreaNotFound) {
		return nil, false, err
	}

	res, err := s.db.Exec(
		`INSERT INTO message_areas (tag, name, description, network, min_sl_read, min_sl_write, pending) VALUES (?, ?, '', ?, 0, 0, 1)`,
		tag, name, network,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			// Lost a race with a concurrent EnsureArea/CreateArea for
			// the same tag -- whoever won already created it.
			existing, err := s.AreaByTag(tag)
			return existing, false, err
		}
		return nil, false, fmt.Errorf("message: ensure area %s: %w", tag, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, fmt.Errorf("message: last insert id: %w", err)
	}
	area, err = s.AreaByID(id)
	return area, true, err
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
		if err := rows.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt, &a.Pending); err != nil {
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
// Yours counts (see AreaWithStats) in a single query. Like ListAreas,
// a pending area never appears here.
func (s *Store) ListAreaStats(securityLevel int, userID int64) ([]AreaWithStats, error) {
	rows, err := s.db.Query(
		`SELECT a.id, a.tag, a.name, a.description, a.network, a.min_sl_read, a.min_sl_write, a.sort_order, a.created_at, a.pending,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id) AS total,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id AND m.from_user_id = ?) AS yours,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id
		           AND NOT EXISTS (SELECT 1 FROM message_reads r
		                           WHERE r.user_id = ? AND r.message_id = m.id)) AS new
		 FROM message_areas a
		 WHERE a.min_sl_read <= ? AND a.pending = 0
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
			&st.Area.MinSLRead, &st.Area.MinSLWrite, &st.Area.SortOrder, &st.Area.CreatedAt, &st.Area.Pending,
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

// ReceiveEcho adds a message internal/tosser tossed in from a remote
// FTN system's echomail -- PostMessage's counterpart for a message
// with no local author account. postedAt is the message's own Written
// timestamp from the packet, not when we happened to receive it.
//
// msgID is the message's MSGID kludge (e.g. "21:3/100 5f3e2a1b"), or
// "" if it carried none. A non-empty msgID already present in areaID
// is treated as a duplicate -- not inserted again, created reports
// false, and the existing message is returned -- since a hub that
// closed its BinkP connection before seeing our M_GOT (a real,
// spec-acknowledged risk; see FTS-1026's PendingFiles requirement and
// internal/binkp's receiveOneFile) will resend the same message on its
// next session. An empty msgID is never deduplicated: some systems
// omit it, and dropping mail from one of those over a missing kludge
// would be worse than the rare accidental duplicate.
func (s *Store) ReceiveEcho(areaID int64, fromName, subject, body, msgID string, postedAt time.Time) (msg *Message, created bool, err error) {
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages (area_id, from_user_id, from_name, to_name, subject, body, msgid, posted_at) VALUES (?, NULL, ?, 'All', ?, ?, ?, ?)`,
		areaID, fromName, subject, body, msgID, postedAt,
	)
	if err != nil {
		return nil, false, fmt.Errorf("message: receive echo to area %d: %w", areaID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("message: receive echo to area %d: rows affected: %w", areaID, err)
	}
	if affected == 0 {
		var id int64
		if err := s.db.QueryRow(`SELECT id FROM messages WHERE area_id = ? AND msgid = ?`, areaID, msgID).Scan(&id); err != nil {
			return nil, false, fmt.Errorf("message: locating duplicate msgid %q in area %d: %w", msgID, areaID, err)
		}
		existing, err := s.MessageByID(id)
		if err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, fmt.Errorf("message: last insert id: %w", err)
	}
	msg, err = s.MessageByID(id)
	if err != nil {
		return nil, false, err
	}
	return msg, true, nil
}

// MessageByID loads a single message, with its local author's current
// username joined in as FromName (a remote author's stored FromName
// is used as-is -- see ReceiveEcho).
func (s *Store) MessageByID(id int64) (*Message, error) {
	row := s.db.QueryRow(
		`SELECT m.id, m.area_id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.to_name, m.subject, m.body, m.posted_at
		 FROM messages m LEFT JOIN users u ON u.id = m.from_user_id WHERE m.id = ?`, id,
	)
	var m Message
	if err := row.Scan(&m.ID, &m.AreaID, &m.FromUserID, &m.FromName, &m.ToName, &m.Subject, &m.Body, &m.PostedAt); err != nil {
		return nil, fmt.Errorf("message: load %d: %w", id, err)
	}
	return &m, nil
}

// ListMessages returns every message in an area, oldest first, with
// each local author's current username joined in as FromName (a
// remote author's stored FromName is used as-is -- see ReceiveEcho).
func (s *Store) ListMessages(areaID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.area_id, m.from_user_id, COALESCE(u.username, m.from_name) AS from_name, m.to_name, m.subject, m.body, m.posted_at
		 FROM messages m LEFT JOIN users u ON u.id = m.from_user_id
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
