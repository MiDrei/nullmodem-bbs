// Package session tracks currently connected BBS sessions ("nodes")
// in the shared SQLite database. The BBS daemon (telnet/SSH) and the
// web admin daemon are separate processes with no other shared
// state, so persisting active sessions here is what lets the BBS's
// own [W]ho's online command and the web dashboard's node-monitoring
// view agree on the same, authoritative list.
package session

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// Node describes one currently connected session.
type Node struct {
	Node        int
	RemoteIP    string
	TermType    string
	Username    string
	ConnectedAt time.Time
}

// Store persists active sessions in the shared SQLite database.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// NewStore wraps an already-opened database handle (see internal/db).
// It does not touch existing rows -- see ClearAll for that -- since
// this constructor runs in two different processes (the BBS daemon
// and the separate web admin daemon) sharing the same database, and
// only one of them may ever legitimately clear it.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// ClearAll removes every currently tracked session. Call this once,
// on BBS daemon startup ONLY -- never from the web admin daemon,
// which runs as a separate process and must not wipe out the BBS
// daemon's live session state just because it (re)started. A freshly
// starting BBS daemon has zero real connections yet, so anything
// already in the table can only be stale from an unclean previous
// shutdown.
func (s *Store) ClearAll() error {
	if _, err := s.db.Exec(`DELETE FROM sessions`); err != nil {
		return fmt.Errorf("session: clear stale sessions: %w", err)
	}
	return nil
}

// Join registers a new session and returns its allocated node number:
// the lowest number not already in use, matching classic multi-node
// BBS software where a node represents a reusable line/slot, not an
// ever-incrementing connection counter. The mutex serializes the
// find-lowest-free-number-then-insert sequence across concurrent
// Join calls from the same process (the only process that ever calls
// Join); it's not needed for SQLite itself, which is opened with a
// single connection (see internal/db) and so already serializes the
// individual statements.
func (s *Store) Join(remoteIP, termType string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT node FROM sessions`)
	if err != nil {
		return 0, fmt.Errorf("session: join: %w", err)
	}
	used := make(map[int]bool)
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return 0, fmt.Errorf("session: join: %w", err)
		}
		used[n] = true
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("session: join: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("session: join: %w", err)
	}

	node := 1
	for used[node] {
		node++
	}

	if _, err := s.db.Exec(
		`INSERT INTO sessions (node, remote_ip, term_type, username) VALUES (?, ?, ?, ?)`,
		node, remoteIP, termType, "(logging in)",
	); err != nil {
		return 0, fmt.Errorf("session: join: %w", err)
	}
	return node, nil
}

// SetUsername updates the username shown for a node once a session
// authenticates.
func (s *Store) SetUsername(node int, username string) error {
	if _, err := s.db.Exec(`UPDATE sessions SET username = ? WHERE node = ?`, username, node); err != nil {
		return fmt.Errorf("session: set username for node %d: %w", node, err)
	}
	return nil
}

// Leave removes a node when its session ends.
func (s *Store) Leave(node int) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE node = ?`, node); err != nil {
		return fmt.Errorf("session: leave node %d: %w", node, err)
	}
	return nil
}

// List returns every currently active node, ordered by node number.
func (s *Store) List() ([]Node, error) {
	rows, err := s.db.Query(`SELECT node, remote_ip, term_type, username, connected_at FROM sessions ORDER BY node`)
	if err != nil {
		return nil, fmt.Errorf("session: list: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.Node, &n.RemoteIP, &n.TermType, &n.Username, &n.ConnectedAt); err != nil {
			return nil, fmt.Errorf("session: scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: list: %w", err)
	}
	return nodes, nil
}
