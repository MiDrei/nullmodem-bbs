// Package binkplog records a complete, human-readable transcript of
// every BinkP session (see internal/binkp.SessionRecorder) so a real
// connectivity problem can be inspected directly afterward instead of
// needing to add temporary debug logging, redeploy, and reproduce it
// again -- exactly the cycle an earlier investigation into a real
// tqwNet/SysopNet handshake failure had to go through. Deliberately
// mirrors internal/archive's own proven shape (bytes on disk, metadata
// + automatic retention in the shared SQLite database) rather than
// inventing a new persistence pattern.
package binkplog

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RetentionPeriod bounds how long a recorded session's transcript
// stays before it's pruned (see Recorder.Finish) -- matches
// internal/archive.RetentionPeriod's own reasoning: long enough to
// notice and investigate something odd, not so long it accumulates
// unbounded on a live system with hourly-ish uplink polls.
const RetentionPeriod = 5 * 24 * time.Hour

// Entry is one recorded session's metadata -- Store.Open returns its
// actual transcript text separately, kept on disk rather than in the
// database.
type Entry struct {
	ID          int64
	Direction   string // "outbound" (we dialed) or "inbound" (they dialed us)
	PeerAddress string
	PeerHost    string
	StartedAt   time.Time
	SizeBytes   int64
	// Outcome is "ok" or "error"; Detail carries the error message
	// (empty for "ok").
	Outcome string
	Detail  string
}

// Store persists session transcripts' bytes under dir and their
// metadata in the shared SQLite database.
type Store struct {
	db  *sql.DB
	dir string
}

// NewStore wraps an already-open database connection; dir is created
// on first use (see Begin), not here.
func NewStore(db *sql.DB, dir string) *Store {
	return &Store{db: db, dir: dir}
}

// Recorder is one in-progress session's transcript, implementing
// binkp.SessionRecorder (via RecordFrame) so it can be set directly as
// a Config.Recorder. Opened via Begin, completed via Finish once the
// caller knows the outcome.
type Recorder struct {
	store       *Store
	mu          sync.Mutex
	file        *os.File
	path        string
	direction   string
	peerAddress string
	peerHost    string
}

// Begin opens a new transcript file on disk and returns a Recorder
// ready to receive frames. Returns an error only for a genuine disk/
// filesystem problem -- callers should treat that as "skip recording
// this session" rather than fail the actual mail exchange over it.
func (s *Store) Begin(direction, peerAddress, peerHost string) (*Recorder, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, fmt.Errorf("binkplog: creating %s: %w", s.dir, err)
	}
	storedName := fmt.Sprintf("%d-%s-%s.txt", time.Now().UnixNano(), direction, sanitizeForFilename(peerAddress))
	path := filepath.Join(s.dir, storedName)
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("binkplog: creating %s: %w", path, err)
	}
	return &Recorder{store: s, file: f, path: path, direction: direction, peerAddress: peerAddress, peerHost: peerHost}, nil
}

// SetPeerAddress updates the peer address Finish will record -- for
// an inbound session, Begin is called before the handshake even
// starts (so a rejected/unrecognized caller still gets a full
// transcript), when the caller's claimed FTN address isn't known yet;
// once authentication resolves it, the caller updates it here before
// calling Finish.
func (r *Recorder) SetPeerAddress(peerAddress string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peerAddress = peerAddress
}

// RecordFrame implements binkp.SessionRecorder -- called concurrently
// from both the send and receive goroutines a real session's transfer
// phase runs (see internal/binkp.session.runTransfer), hence the
// mutex. Write errors are swallowed: a broken transcript is never a
// reason to fail the real mail exchange it's merely observing.
func (r *Recorder) RecordFrame(direction, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.file, "%s %-4s %s\n", time.Now().Format("15:04:05.000"), strings.ToUpper(direction), line)
}

// Finish closes the transcript and records it, with outcome/detail
// describing how the session actually ended -- always safe to call,
// even after zero frames were ever recorded (a session that failed
// before exchanging anything is still useful evidence of that). Also
// prunes every entry older than RetentionPeriod (see Store.Prune),
// piggybacked here the same way archive.Capture.Finish does it,
// rather than a separate timer.
func (r *Recorder) Finish(outcome, detail string) (*Entry, error) {
	info, statErr := r.file.Stat()
	closeErr := r.file.Close()
	if statErr != nil {
		return nil, fmt.Errorf("binkplog: stat %s: %w", r.path, statErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("binkplog: close %s: %w", r.path, closeErr)
	}

	res, err := r.store.db.Exec(
		`INSERT INTO binkp_sessions (direction, peer_address, peer_host, size_bytes, storage_path, outcome, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.direction, r.peerAddress, r.peerHost, info.Size(), r.path, outcome, detail,
	)
	if err != nil {
		return nil, fmt.Errorf("binkplog: recording session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("binkplog: recording session: %w", err)
	}

	// Best-effort: a pruning failure shouldn't hide the entry just
	// successfully recorded.
	_ = r.store.Prune(time.Now())

	return r.store.ByID(id)
}

// ByID loads one entry's metadata.
func (s *Store) ByID(id int64) (*Entry, error) {
	row := s.db.QueryRow(
		`SELECT id, direction, peer_address, peer_host, started_at, size_bytes, outcome, detail
		 FROM binkp_sessions WHERE id = ?`, id,
	)
	var e Entry
	if err := row.Scan(&e.ID, &e.Direction, &e.PeerAddress, &e.PeerHost, &e.StartedAt, &e.SizeBytes, &e.Outcome, &e.Detail); err != nil {
		return nil, fmt.Errorf("binkplog: entry %d: %w", id, err)
	}
	return &e, nil
}

// Recent returns the most recently started sessions, newest first.
func (s *Store) Recent(limit int) ([]Entry, error) {
	rows, err := s.db.Query(
		`SELECT id, direction, peer_address, peer_host, started_at, size_bytes, outcome, detail
		 FROM binkp_sessions ORDER BY started_at DESC, id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("binkplog: listing sessions: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Direction, &e.PeerAddress, &e.PeerHost, &e.StartedAt, &e.SizeBytes, &e.Outcome, &e.Detail); err != nil {
			return nil, fmt.Errorf("binkplog: scanning session: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("binkplog: listing sessions: %w", err)
	}
	return out, nil
}

// Open returns id's raw transcript text.
func (s *Store) Open(id int64) (io.ReadCloser, error) {
	var path string
	if err := s.db.QueryRow(`SELECT storage_path FROM binkp_sessions WHERE id = ?`, id).Scan(&path); err != nil {
		return nil, fmt.Errorf("binkplog: entry %d: %w", id, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("binkplog: opening %s: %w", path, err)
	}
	return f, nil
}

// Prune deletes every entry older than RetentionPeriod as of now,
// along with its transcript file on disk. The age comparison runs in
// Go against each row's own started_at (read back through the driver,
// not re-derived from a Go-side time.Time bound as a query parameter
// -- see internal/archive.Store.Prune's own doc comment for why) -- a
// full scan of what's meant to be a small, bounded table, so this
// costs nothing worth avoiding.
func (s *Store) Prune(now time.Time) error {
	rows, err := s.db.Query(`SELECT id, storage_path, started_at FROM binkp_sessions`)
	if err != nil {
		return fmt.Errorf("binkplog: finding entries to prune: %w", err)
	}
	type victim struct {
		id   int64
		path string
	}
	var victims []victim
	for rows.Next() {
		var id int64
		var path string
		var startedAt time.Time
		if err := rows.Scan(&id, &path, &startedAt); err != nil {
			rows.Close()
			return fmt.Errorf("binkplog: scanning entry to prune: %w", err)
		}
		if now.Sub(startedAt) > RetentionPeriod {
			victims = append(victims, victim{id, path})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("binkplog: finding entries to prune: %w", err)
	}
	rows.Close()

	for _, v := range victims {
		if _, err := s.db.Exec(`DELETE FROM binkp_sessions WHERE id = ?`, v.id); err != nil {
			return fmt.Errorf("binkplog: pruning entry %d: %w", v.id, err)
		}
		if err := os.Remove(v.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("binkplog: removing pruned file %s: %w", v.path, err)
		}
	}
	return nil
}

// sanitizeForFilename strips anything that isn't a plain ASCII
// letter/digit/./-/@/: from addr before it's used as part of a local
// filename -- an FTN address itself is already narrow (digits, ':',
// '/', '.', '@', letters for a network tag), this just drops the
// path-separator-like '/' rather than letting it split into
// directories, and is defensive against a peer-controlled peerHost
// string (an inbound session's remote address) containing anything
// stranger.
func sanitizeForFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '@', r == ':':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	if b.Len() > 64 {
		return b.String()[:64]
	}
	return b.String()
}
