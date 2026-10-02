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
	// Hidden marks a data area -- one programs write to and read, not
	// people (fsxNet's FSX_DAT: InterBBS last callers, oneliners). It
	// is tossed and forwarded as usual but left out of every caller's
	// area list (Telnet/SSH, portal, reader, QWK), so its traffic never
	// shows as unread. The web admin still lists it.
	Hidden bool
	// KeepDays and KeepMax are this area's own cleanup limits (see
	// internal/maintenance): 0 means the configured default, -1 keep
	// everything, more a limit in days / messages.
	KeepDays int
	KeepMax  int
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
	// MsgID is this message's permanent, network-wide unique
	// identifier (FTS-0009's "^AMSGID" kludge value, e.g.
	// "21:3/100 5f3e2a1b") for a message tossed in from a remote
	// system -- preserved unchanged through every hop, never
	// regenerated (see ReceiveEcho/echoMsgID). Empty for a message
	// posted locally and never yet sent anywhere: internal/tosser
	// derives one deterministically from this system's own address
	// and the message's own ID only at send time (see buildPacket),
	// rather than persisting it here, since it's fully reproducible
	// from data already on the row.
	MsgID string
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
		`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending, hidden, keep_days, keep_max
		 FROM message_areas WHERE id = ?`, id,
	))
}

// AreaByTag loads a single area by its short tag (case-insensitive).
func (s *Store) AreaByTag(tag string) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending, hidden, keep_days, keep_max
		 FROM message_areas WHERE tag = ?`, tag,
	))
}

func (s *Store) scanArea(row *sql.Row) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt, &a.Pending, &a.Hidden, &a.KeepDays, &a.KeepMax); err != nil {
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
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending, hidden, keep_days, keep_max
		 FROM message_areas WHERE min_sl_read <= ? AND pending = 0 AND hidden = 0 ORDER BY network, sort_order, name`, securityLevel)
}

// AllAreas returns every non-pending area regardless of SL gating,
// for the web admin area management UI, in the same network-grouped
// order as ListAreas. See PendingAreas for the areas this excludes.
func (s *Store) AllAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending, hidden, keep_days, keep_max
		 FROM message_areas WHERE pending = 0 ORDER BY network, sort_order, name`)
}

// PendingAreas returns every area awaiting sysop review (see
// EnsureArea), oldest first -- what the web admin's Pending Areas page
// lists for approval.
func (s *Store) PendingAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_read, min_sl_write, sort_order, created_at, pending, hidden, keep_days, keep_max
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
		if err := rows.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLRead, &a.MinSLWrite, &a.SortOrder, &a.CreatedAt, &a.Pending, &a.Hidden, &a.KeepDays, &a.KeepMax); err != nil {
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
		`SELECT a.id, a.tag, a.name, a.description, a.network, a.min_sl_read, a.min_sl_write, a.sort_order, a.created_at, a.pending, a.hidden, a.keep_days, a.keep_max,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id) AS total,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id AND m.from_user_id = ?) AS yours,
		        (SELECT COUNT(1) FROM messages m WHERE m.area_id = a.id
		           AND NOT EXISTS (SELECT 1 FROM message_reads r
		                           WHERE r.user_id = ? AND r.message_id = m.id)) AS new
		 FROM message_areas a
		 WHERE a.min_sl_read <= ? AND a.pending = 0 AND a.hidden = 0
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
			&st.Area.MinSLRead, &st.Area.MinSLWrite, &st.Area.SortOrder, &st.Area.CreatedAt, &st.Area.Pending, &st.Area.Hidden, &st.Area.KeepDays, &st.Area.KeepMax,
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

// MarkAreaRead records every message in areaID as read by userID --
// for an area nobody reads message by message (FSX_DAT's data posts).
// It returns how many were newly marked.
func (s *Store) MarkAreaRead(userID, areaID int64) (int64, error) {
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO message_reads (user_id, message_id) SELECT ?, id FROM messages WHERE area_id = ?`,
		userID, areaID,
	)
	if err != nil {
		return 0, fmt.Errorf("message: mark area %d read for user %d: %w", areaID, userID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// QWKSelectedAreaIDs returns the set of area IDs userID has explicitly
// chosen to include in their QWK offline-mail packets. An empty
// (non-nil) map means the user has never configured a selection --
// callers should treat that as "include every readable area", not
// "include none".
func (s *Store) QWKSelectedAreaIDs(userID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT area_id FROM qwk_area_selections WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("message: qwk selected areas for user %d: %w", userID, err)
	}
	defer rows.Close()

	selected := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("message: scan qwk selected area: %w", err)
		}
		selected[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: qwk selected areas for user %d: %w", userID, err)
	}
	return selected, nil
}

// SetQWKSelectedAreas replaces userID's QWK area selection with
// exactly areaIDs. Passing an empty slice clears the selection
// entirely, which reverts to the "include every readable area"
// default -- see QWKSelectedAreaIDs.
func (s *Store) SetQWKSelectedAreas(userID int64, areaIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("message: set qwk selected areas for user %d: %w", userID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM qwk_area_selections WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("message: clear qwk selected areas for user %d: %w", userID, err)
	}
	for _, areaID := range areaIDs {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO qwk_area_selections (user_id, area_id) VALUES (?, ?)`,
			userID, areaID,
		); err != nil {
			return fmt.Errorf("message: set qwk selected area %d for user %d: %w", areaID, userID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("message: set qwk selected areas for user %d: %w", userID, err)
	}
	return nil
}

// RenameNetwork moves every area whose network is from (compared
// without regard to case) to network to, returning how many -- see
// config.Config.NetworkRenames.
func (s *Store) RenameNetwork(from, to string) (int64, error) {
	res, err := s.db.Exec(`UPDATE message_areas SET network = ? WHERE LOWER(network) = LOWER(?) AND network != ?`, to, from, to)
	if err != nil {
		return 0, fmt.Errorf("message: renaming network %q to %q: %w", from, to, err)
	}
	return res.RowsAffected()
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
	// The poster obviously doesn't need their own post flagged "New"
	// to themselves -- without this, ListAreaStats/ReadMessageIDs
	// counted it as unread until they happened to open it, same as
	// anyone else's message.
	if err := s.MarkMessageRead(fromUserID, id); err != nil {
		return nil, err
	}
	return s.MessageByID(id)
}

// PendingEcho is one echo message ready to be handed to a BinkP peer,
// paired with its area's tag (needed for the AREA: kludge line
// internal/tosser writes ahead of the body) -- returned by
// PendingOutboundEcho (this system's own local posts, sent upward to
// its configured uplink for that network) and by internal/tosser's
// RoutedOutboundEchoForward (any message in an area a downlink has
// subscribed to, local or remote origin alike, sent downward via
// SEEN-BY tracking instead -- see MarkSeenBy), kept separate from the
// general Message API since nothing else needs the tag riding along.
type PendingEcho struct {
	Message
	AreaTag string
}

// PendingOutboundEcho returns locally-posted messages (never one
// tossed in from a remote system -- see Message.IsFromRemote) in
// areas whose network matches (case-insensitively) network, that
// haven't been handed to an uplink yet, oldest first -- what
// internal/tosser bundles into an outbound packet alongside any
// pending netmail. network must be non-empty (an empty network,
// meaning a local-only area, never has anywhere to send to) or this
// returns nothing at all rather than matching every arealess post.
func (s *Store) PendingOutboundEcho(network string) ([]PendingEcho, error) {
	if network == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT m.id, m.area_id, m.from_user_id, COALESCE(NULLIF(m.from_name, ''), u.username) AS from_name, m.to_name, m.subject, m.body, m.posted_at, a.tag
		 FROM messages m
		 JOIN message_areas a ON a.id = m.area_id
		 LEFT JOIN users u ON u.id = m.from_user_id
		 WHERE m.from_user_id IS NOT NULL AND m.sent_at IS NULL AND a.network != '' AND LOWER(a.network) = LOWER(?)
		 ORDER BY m.posted_at ASC, m.id ASC`,
		network,
	)
	if err != nil {
		return nil, fmt.Errorf("message: pending outbound echo for network %q: %w", network, err)
	}
	defer rows.Close()

	var out []PendingEcho
	for rows.Next() {
		var p PendingEcho
		if err := rows.Scan(&p.ID, &p.AreaID, &p.FromUserID, &p.FromName, &p.ToName, &p.Subject, &p.Body, &p.PostedAt, &p.AreaTag); err != nil {
			return nil, fmt.Errorf("message: scan pending outbound echo row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: pending outbound echo for network %q: %w", network, err)
	}
	return out, nil
}

// MarkSent records that messageID was successfully handed off to (and
// acknowledged by) an uplink -- see internal/tosser.Poll. Idempotent.
func (s *Store) MarkSent(messageID int64) error {
	if _, err := s.db.Exec(
		`UPDATE messages SET sent_at = CURRENT_TIMESTAMP WHERE id = ? AND sent_at IS NULL`,
		messageID,
	); err != nil {
		return fmt.Errorf("message: mark %d sent: %w", messageID, err)
	}
	return nil
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
		// UTC, like CURRENT_TIMESTAMP: posted_at is compared and sorted
		// as text, and a local time would be off by its offset.
		areaID, fromName, subject, body, msgID, postedAt.UTC(),
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
		`SELECT m.id, m.area_id, m.from_user_id, COALESCE(NULLIF(m.from_name, ''), u.username) AS from_name, m.to_name, m.subject, m.body, m.posted_at, m.msgid
		 FROM messages m LEFT JOIN users u ON u.id = m.from_user_id WHERE m.id = ?`, id,
	)
	var m Message
	if err := row.Scan(&m.ID, &m.AreaID, &m.FromUserID, &m.FromName, &m.ToName, &m.Subject, &m.Body, &m.PostedAt, &m.MsgID); err != nil {
		return nil, fmt.Errorf("message: load %d: %w", id, err)
	}
	return &m, nil
}

// ListMessages returns every message in an area, oldest first, with
// each local author's current username joined in as FromName (a
// remote author's stored FromName is used as-is -- see ReceiveEcho).
func (s *Store) ListMessages(areaID int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.area_id, m.from_user_id, COALESCE(NULLIF(m.from_name, ''), u.username) AS from_name, m.to_name, m.subject, m.body, m.posted_at, m.msgid
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
		if err := rows.Scan(&m.ID, &m.AreaID, &m.FromUserID, &m.FromName, &m.ToName, &m.Subject, &m.Body, &m.PostedAt, &m.MsgID); err != nil {
			return nil, fmt.Errorf("message: scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message: list messages for area %d: %w", areaID, err)
	}
	return messages, nil
}

// UnreadMessages returns the messages in areaID userID hasn't read,
// oldest first -- what a new scan reads (internal/bbs).
func (s *Store) UnreadMessages(areaID, userID int64) ([]Message, error) {
	return s.queryMessages(`WHERE m.area_id = ? AND NOT EXISTS
		(SELECT 1 FROM message_reads r WHERE r.user_id = ? AND r.message_id = m.id)
		ORDER BY m.posted_at, m.id`, areaID, userID)
}

// UnreadToUser returns the unread messages addressed to one of names
// (the user's handle and real name, ignoring case) in the areas
// securityLevel may read, oldest first -- not their own posts, and
// not in data areas (hidden).
func (s *Store) UnreadToUser(userID int64, securityLevel int, names []string) ([]Message, error) {
	var match []string
	args := []any{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			match = append(match, `m.to_name = ? COLLATE NOCASE`)
			args = append(args, n)
		}
	}
	if len(match) == 0 {
		return nil, nil
	}
	args = append(args, userID, securityLevel, userID)
	return s.queryMessages(`JOIN message_areas a ON a.id = m.area_id
		WHERE (`+strings.Join(match, " OR ")+`)
		AND (m.from_user_id IS NULL OR m.from_user_id <> ?)
		AND a.min_sl_read <= ? AND a.pending = 0 AND a.hidden = 0
		AND NOT EXISTS (SELECT 1 FROM message_reads r WHERE r.user_id = ? AND r.message_id = m.id)
		ORDER BY m.posted_at, m.id`, args...)
}

// Search finds messages whose subject, text, sender or recipient
// contains q (ignoring case), newest first, in the areas
// securityLevel may read -- or only areaID, if set -- leaving out data
// areas (hidden).
func (s *Store) Search(securityLevel int, q string, areaID int64, limit int) ([]Message, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	where := `JOIN message_areas a ON a.id = m.area_id
		WHERE a.min_sl_read <= ? AND a.pending = 0 AND a.hidden = 0
		AND (instr(lower(m.subject), lower(?)) > 0 OR instr(lower(m.body), lower(?)) > 0
			OR instr(lower(m.from_name), lower(?)) > 0 OR instr(lower(m.to_name), lower(?)) > 0
			OR instr(lower(COALESCE(u.username, '')), lower(?)) > 0)`
	args := []any{securityLevel, q, q, q, q, q}
	if areaID > 0 {
		where += ` AND m.area_id = ?`
		args = append(args, areaID)
	}
	args = append(args, limit)
	return s.queryMessages(where+` ORDER BY m.posted_at DESC, m.id DESC LIMIT ?`, args...)
}

// queryMessages is ListMessages' SELECT with its own WHERE/ORDER.
func (s *Store) queryMessages(rest string, args ...any) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT m.id, m.area_id, m.from_user_id, COALESCE(NULLIF(m.from_name, ''), u.username) AS from_name, m.to_name, m.subject, m.body, m.posted_at, m.msgid
		 FROM messages m LEFT JOIN users u ON u.id = m.from_user_id `+rest, args...)
	if err != nil {
		return nil, fmt.Errorf("message: query messages: %w", err)
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.AreaID, &m.FromUserID, &m.FromName, &m.ToName, &m.Subject, &m.Body, &m.PostedAt, &m.MsgID); err != nil {
			return nil, fmt.Errorf("message: scan message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// FirstUnreadPosition returns the 0-based position, in the area's own
// oldest-first order, of the first message userID hasn't read yet --
// mirrors internal/bbs's own firstUnreadIndex, which jumps the
// Telnet/SSH area lightbar straight there on entry instead of always
// landing on the oldest message ever posted, so the web BBS portal's
// paginated list (see ListMessagesPage) can compute which page to
// open on too. Falls back to the position of the newest (last)
// message once everything is already read, and 0 for an empty area.
//
// The position-counting query compares directly against the first
// unread row's own stored (posted_at, id) via a subquery, the same
// way Neighbors does and for the same reason: a Go-side time.Time
// bound as a separate parameter doesn't reliably compare against
// CURRENT_TIMESTAMP-populated TEXT it didn't itself produce.
func (s *Store) FirstUnreadPosition(areaID, userID int64) (int, error) {
	var firstUnreadID int64
	switch err := s.db.QueryRow(
		`SELECT m.id FROM messages m
		 WHERE m.area_id = ? AND NOT EXISTS (SELECT 1 FROM message_reads r WHERE r.user_id = ? AND r.message_id = m.id)
		 ORDER BY m.posted_at ASC, m.id ASC LIMIT 1`,
		areaID, userID,
	).Scan(&firstUnreadID); {
	case errors.Is(err, sql.ErrNoRows):
		var total int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM messages WHERE area_id = ?`, areaID).Scan(&total); err != nil {
			return 0, fmt.Errorf("message: first unread position: count: %w", err)
		}
		if total == 0 {
			return 0, nil
		}
		return total - 1, nil
	case err != nil:
		return 0, fmt.Errorf("message: first unread position: %w", err)
	}

	var position int
	if err := s.db.QueryRow(
		`SELECT COUNT(1) FROM messages WHERE area_id = ?
		 AND (posted_at, id) < (SELECT posted_at, id FROM messages WHERE id = ?)`,
		areaID, firstUnreadID,
	).Scan(&position); err != nil {
		return 0, fmt.Errorf("message: first unread position: count before: %w", err)
	}
	return position, nil
}

// ListMessagesPage is ListMessages with LIMIT/OFFSET pagination and a
// total row count, for the web BBS portal's paginated message list --
// unlike the telnet/ssh session (which loads a whole area at once for
// its own in-terminal paging via ListMessages, left untouched here),
// a JSON API shouldn't ship potentially thousands of messages in one
// response. Ordering matches ListMessages (oldest first, the
// conventional echo-area reading order).
func (s *Store) ListMessagesPage(areaID int64, limit, offset int) ([]Message, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM messages WHERE area_id = ?`, areaID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("message: count messages for area %d: %w", areaID, err)
	}

	rows, err := s.db.Query(
		`SELECT m.id, m.area_id, m.from_user_id, COALESCE(NULLIF(m.from_name, ''), u.username) AS from_name, m.to_name, m.subject, m.body, m.posted_at, m.msgid
		 FROM messages m LEFT JOIN users u ON u.id = m.from_user_id
		 WHERE m.area_id = ? ORDER BY m.posted_at, m.id LIMIT ? OFFSET ?`, areaID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("message: list messages page for area %d: %w", areaID, err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.AreaID, &m.FromUserID, &m.FromName, &m.ToName, &m.Subject, &m.Body, &m.PostedAt, &m.MsgID); err != nil {
			return nil, 0, fmt.Errorf("message: scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("message: list messages page for area %d: %w", areaID, err)
	}
	return messages, total, nil
}

// Neighbors returns the IDs of the messages immediately before and
// after id within areaID, in the same oldest-first order
// ListMessages/ListMessagesPage use -- the web BBS portal's reader
// uses this for Prev/Next navigation between messages without
// needing to hold (or re-fetch) the whole area's message list just
// to find what comes next. Either return value is nil at the
// beginning/end of the area.
//
// The (posted_at, id) row-value comparisons below compare id's
// posted_at directly against the other candidate rows' own stored
// posted_at, both read from the same column -- deliberately not a
// Go-side time.Time bound as a separate query parameter, which
// doesn't reliably compare equal (or order correctly) against
// CURRENT_TIMESTAMP-populated TEXT it didn't itself produce: confirmed
// live, ordering broke because the two representations didn't match.
func (s *Store) Neighbors(areaID, id int64) (before, after *int64, err error) {
	var b int64
	switch err := s.db.QueryRow(
		`SELECT id FROM messages WHERE area_id = ?
		 AND (posted_at, id) < (SELECT posted_at, id FROM messages WHERE id = ?)
		 ORDER BY posted_at DESC, id DESC LIMIT 1`,
		areaID, id,
	).Scan(&b); {
	case err == nil:
		before = &b
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, nil, fmt.Errorf("message: neighbors before %d: %w", id, err)
	}

	var a int64
	switch err := s.db.QueryRow(
		`SELECT id FROM messages WHERE area_id = ?
		 AND (posted_at, id) > (SELECT posted_at, id FROM messages WHERE id = ?)
		 ORDER BY posted_at ASC, id ASC LIMIT 1`,
		areaID, id,
	).Scan(&a); {
	case err == nil:
		after = &a
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, nil, fmt.Errorf("message: neighbors after %d: %w", id, err)
	}
	return before, after, nil
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

// PostEcho stores an echomail message written by local user
// fromUserID somewhere other than the BBS itself -- a point's reader
// app posting as that user (see internal/tosser's points.go) -- so it
// goes out like any local post, under this system's own address.
// msgID is the point's own MSGID, kept only to recognize the point
// resending the same packet (created is false then, as in
// ReceiveEcho); outbound, the message gets this system's MSGID.
func (s *Store) PostEcho(areaID, fromUserID int64, toName, subject, body, msgID string, postedAt time.Time) (msg *Message, created bool, err error) {
	if toName == "" {
		toName = "All"
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages (area_id, from_user_id, to_name, subject, body, msgid, posted_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		areaID, fromUserID, toName, subject, body, msgID, postedAt.UTC(), // UTC: see ReceiveEcho
	)
	if err != nil {
		return nil, false, fmt.Errorf("message: post echo to area %d: %w", areaID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, false, fmt.Errorf("message: post echo to area %d: %w", areaID, err)
	} else if n == 0 {
		var id int64
		if err := s.db.QueryRow(`SELECT id FROM messages WHERE area_id = ? AND msgid = ?`, areaID, msgID).Scan(&id); err != nil {
			return nil, false, fmt.Errorf("message: locating duplicate msgid %q in area %d: %w", msgID, areaID, err)
		}
		existing, err := s.MessageByID(id)
		return existing, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, fmt.Errorf("message: last insert id: %w", err)
	}
	if err := s.MarkMessageRead(fromUserID, id); err != nil {
		return nil, false, err
	}
	msg, err = s.MessageByID(id)
	return msg, err == nil, err
}

// DeliveredToPoint returns the IDs of the messages already sent to the
// point whose uplink entry has host uplinkHost.
func (s *Store) DeliveredToPoint(uplinkHost string) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT message_id FROM echo_point_deliveries WHERE uplink_host = ?`, uplinkHost)
	if err != nil {
		return nil, fmt.Errorf("message: point deliveries for %s: %w", uplinkHost, err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("message: point deliveries for %s: %w", uplinkHost, err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// MarkDeliveredToPoint records that messageID reached (or came from)
// the point with host uplinkHost. Idempotent.
func (s *Store) MarkDeliveredToPoint(messageID int64, uplinkHost string) error {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO echo_point_deliveries (message_id, uplink_host) VALUES (?, ?)`, messageID, uplinkHost); err != nil {
		return fmt.Errorf("message: mark %d delivered to %s: %w", messageID, uplinkHost, err)
	}
	return nil
}

// SetAreaHidden marks area id as a data area (see Area.Hidden), or
// back as an ordinary one.
func (s *Store) SetAreaHidden(id int64, hidden bool) error {
	if _, err := s.db.Exec(`UPDATE message_areas SET hidden = ? WHERE id = ?`, hidden, id); err != nil {
		return fmt.Errorf("message: set area %d hidden: %w", id, err)
	}
	return nil
}

// PostMessageAs is PostMessage under another sender name than the
// poster's username -- for a message the system posts through a local
// account (its sysop), like an InterBBS last-callers record whose
// sender must read "ibbslastcall". It goes out like any local post.
func (s *Store) PostMessageAs(areaID, fromUserID int64, fromName, toName, subject, body string) (*Message, error) {
	res, err := s.db.Exec(
		`INSERT INTO messages (area_id, from_user_id, from_name, to_name, subject, body) VALUES (?, ?, ?, ?, ?, ?)`,
		areaID, fromUserID, fromName, toName, subject, body,
	)
	if err != nil {
		return nil, fmt.Errorf("message: post to area %d: %w", areaID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("message: last insert id: %w", err)
	}
	if err := s.MarkMessageRead(fromUserID, id); err != nil {
		return nil, err
	}
	return s.MessageByID(id)
}

// SubjectBodies returns the newest messages in areaID whose subject
// is one of subjects (case-insensitive), newest first, at most limit
// -- the raw material for InterBBS lists (internal/lastcallers).
func (s *Store) SubjectBodies(areaID int64, subjects []string, limit int) ([]Message, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	q := `SELECT id, subject, body, posted_at FROM messages WHERE area_id = ? AND LOWER(subject) IN (?` + strings.Repeat(`, ?`, len(subjects)-1) + `) ORDER BY id DESC LIMIT ?`
	args := []any{areaID}
	for _, sub := range subjects {
		args = append(args, strings.ToLower(sub))
	}
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("message: messages by subject in area %d: %w", areaID, err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Subject, &m.Body, &m.PostedAt); err != nil {
			return nil, fmt.Errorf("message: messages by subject in area %d: %w", areaID, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetAreaKeep sets an area's own cleanup limits (see Area.KeepDays).
func (s *Store) SetAreaKeep(id int64, days, max int) error {
	if _, err := s.db.Exec(`UPDATE message_areas SET keep_days = ?, keep_max = ? WHERE id = ?`, days, max, id); err != nil {
		return fmt.Errorf("message: set area %d limits: %w", id, err)
	}
	return nil
}
