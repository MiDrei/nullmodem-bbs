// Package archive retains a short-lived raw copy of every inbound
// BinkP file (an FTS-0001 packet/bundle, a TIC descriptor, a
// file-echo payload, or anything unsupported) exactly as received,
// mirroring how binkd/ifcico-style tossers keep a "bad packets"
// directory for later inspection -- except this keeps everything, not
// just failures, since something that tossed cleanly today can still
// turn out to matter later (this exists precisely because a real
// inbound TIC file's missing description needed the actual bytes to
// settle, and by the time that was noticed, the file long since
// stopped being an ordinary BinkP session's transient in-memory
// buffer). Retained for RetentionPeriod, then pruned automatically;
// the web admin's Packet Analyzer page is the primary way to browse,
// inspect, and re-toss an entry.
package archive

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RetentionPeriod bounds how long a captured file stays before it's
// pruned (see Capture.Finish) -- 5 days chosen as "long enough to
// notice and investigate something odd, not so long it accumulates
// unbounded" for a live BBS with real day-to-day FTN traffic.
var RetentionPeriod = 5 * 24 * time.Hour // settable: config maintenance

// Entry is one archived inbound file's metadata -- Store.Open returns
// its actual bytes separately, kept on disk rather than in the
// database.
type Entry struct {
	ID            int64
	Filename      string
	UplinkAddress string
	UplinkHost    string
	SizeBytes     int64
	ReceivedAt    time.Time
	// Outcome is "ok", "skipped", or "error" -- see
	// internal/tosser's handleInboundFile, the only writer.
	Outcome string
	// Detail carries the error message (Outcome "error") or is empty
	// otherwise.
	Detail string
}

// Store persists archived files' bytes under dir and their metadata
// in the shared SQLite database.
type Store struct {
	db  *sql.DB
	dir string
}

// NewStore wraps an already-open database connection; dir is created
// on first use (see Begin), not here.
func NewStore(db *sql.DB, dir string) *Store {
	return &Store{db: db, dir: dir}
}

// Capture is one in-progress archive write, opened via Begin and
// completed via Finish once the caller knows the outcome.
type Capture struct {
	store         *Store
	file          *os.File
	path          string
	filename      string
	uplinkAddress string
	uplinkHost    string
}

// Begin opens a new capture file on disk for filename and returns a
// Capture whose Writer should have inbound bytes teed into it as
// they're read from the peer. Returns an error only for a genuine
// disk/filesystem problem -- callers should treat that as "skip
// archiving this one" rather than fail the actual mail processing
// over it.
func (s *Store) Begin(uplinkAddress, uplinkHost, filename string) (*Capture, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, fmt.Errorf("archive: creating %s: %w", s.dir, err)
	}
	storedName := fmt.Sprintf("%d-%s", time.Now().UnixNano(), sanitizeFilename(filename))
	path := filepath.Join(s.dir, storedName)
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("archive: creating %s: %w", path, err)
	}
	return &Capture{store: s, file: f, path: path, filename: filename, uplinkAddress: uplinkAddress, uplinkHost: uplinkHost}, nil
}

// Writer returns the destination inbound bytes should be teed into.
func (c *Capture) Writer() io.Writer { return c.file }

// Finish closes the capture and records it, with outcome/detail
// describing what tossing decided -- always safe to call, even for a
// zero-byte or partial file (e.g. the session aborted mid-transfer):
// a partial capture is still useful evidence of exactly that kind of
// failure. Also prunes every entry older than RetentionPeriod (see
// Store.Prune) -- piggybacked here rather than run on a separate
// timer, since Finish already runs once per inbound file and that's
// a perfectly adequate cadence to stay bounded.
func (c *Capture) Finish(outcome, detail string) (*Entry, error) {
	info, statErr := c.file.Stat()
	closeErr := c.file.Close()
	if statErr != nil {
		return nil, fmt.Errorf("archive: stat %s: %w", c.path, statErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("archive: close %s: %w", c.path, closeErr)
	}

	res, err := c.store.db.Exec(
		`INSERT INTO inbound_archive (filename, uplink_address, uplink_host, size_bytes, received_at, storage_path, outcome, detail)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?)`,
		c.filename, c.uplinkAddress, c.uplinkHost, info.Size(), c.path, outcome, detail,
	)
	if err != nil {
		return nil, fmt.Errorf("archive: recording %s: %w", c.filename, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("archive: recording %s: %w", c.filename, err)
	}

	// Best-effort: a pruning failure shouldn't hide the entry just
	// successfully recorded.
	_ = c.store.Prune(time.Now())

	return c.store.ByID(id)
}

// ByID loads one entry's metadata.
func (s *Store) ByID(id int64) (*Entry, error) {
	row := s.db.QueryRow(
		`SELECT id, filename, uplink_address, uplink_host, size_bytes, received_at, outcome, detail
		 FROM inbound_archive WHERE id = ?`, id,
	)
	var e Entry
	if err := row.Scan(&e.ID, &e.Filename, &e.UplinkAddress, &e.UplinkHost, &e.SizeBytes, &e.ReceivedAt, &e.Outcome, &e.Detail); err != nil {
		return nil, fmt.Errorf("archive: entry %d: %w", id, err)
	}
	return &e, nil
}

// List returns a page of entries, most recently captured first,
// alongside the total count across all entries (for pagination).
func (s *Store) List(limit, offset int) ([]Entry, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM inbound_archive`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("archive: counting entries: %w", err)
	}
	rows, err := s.db.Query(
		`SELECT id, filename, uplink_address, uplink_host, size_bytes, received_at, outcome, detail
		 FROM inbound_archive ORDER BY received_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("archive: listing entries: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Filename, &e.UplinkAddress, &e.UplinkHost, &e.SizeBytes, &e.ReceivedAt, &e.Outcome, &e.Detail); err != nil {
			return nil, 0, fmt.Errorf("archive: scanning entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("archive: listing entries: %w", err)
	}
	return out, total, nil
}

// Open returns id's raw captured bytes, for inspection or re-tossing.
func (s *Store) Open(id int64) (io.ReadCloser, error) {
	var path string
	if err := s.db.QueryRow(`SELECT storage_path FROM inbound_archive WHERE id = ?`, id).Scan(&path); err != nil {
		return nil, fmt.Errorf("archive: entry %d: %w", id, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("archive: opening %s: %w", path, err)
	}
	return f, nil
}

// Delete removes id's row and its file on disk.
func (s *Store) Delete(id int64) error {
	var path string
	if err := s.db.QueryRow(`SELECT storage_path FROM inbound_archive WHERE id = ?`, id).Scan(&path); err != nil {
		return fmt.Errorf("archive: entry %d: %w", id, err)
	}
	if _, err := s.db.Exec(`DELETE FROM inbound_archive WHERE id = ?`, id); err != nil {
		return fmt.Errorf("archive: deleting entry %d: %w", id, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("archive: removing %s: %w", path, err)
	}
	return nil
}

// Prune deletes every entry older than RetentionPeriod as of now,
// along with its file on disk. The age comparison runs in Go against
// each row's own received_at (read back through the driver, not
// re-derived from a Go-side time.Time bound as a query parameter,
// which doesn't reliably compare against CURRENT_TIMESTAMP-populated
// TEXT it didn't itself produce -- see internal/message.Store.
// Neighbors' own doc comment for the same gotcha) -- a full scan of
// what's meant to be a small, bounded table, so this costs nothing
// worth avoiding.
func (s *Store) Prune(now time.Time) error {
	_, err := s.PruneOlderThan(now, RetentionPeriod, false)
	return err
}

// PruneOlderThan deletes (or with dry, only counts) the entries older
// than age as of now, with their files -- Prune with a given age, for
// internal/maintenance.
func (s *Store) PruneOlderThan(now time.Time, age time.Duration, dry bool) (int, error) {
	rows, err := s.db.Query(`SELECT id, storage_path, received_at FROM inbound_archive`)
	if err != nil {
		return 0, fmt.Errorf("archive: finding entries to prune: %w", err)
	}
	type victim struct {
		id   int64
		path string
	}
	var victims []victim
	for rows.Next() {
		var id int64
		var path string
		var receivedAt time.Time
		if err := rows.Scan(&id, &path, &receivedAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("archive: scanning entry to prune: %w", err)
		}
		if now.Sub(receivedAt) > age {
			victims = append(victims, victim{id, path})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("archive: finding entries to prune: %w", err)
	}
	rows.Close()

	if dry {
		return len(victims), nil
	}
	for _, v := range victims {
		if _, err := s.db.Exec(`DELETE FROM inbound_archive WHERE id = ?`, v.id); err != nil {
			return 0, fmt.Errorf("archive: pruning entry %d: %w", v.id, err)
		}
		if err := os.Remove(v.path); err != nil && !os.IsNotExist(err) {
			return 0, fmt.Errorf("archive: removing pruned file %s: %w", v.path, err)
		}
	}
	return len(victims), nil
}

// sanitizeFilename strips path separators and NUL from a peer-
// supplied filename before it's used as (part of) a local path --
// the same defensive posture internal/file.Store.storeFile already
// takes on an uploaded/received file's name.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r == 0 {
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "file"
	}
	return b.String()
}
