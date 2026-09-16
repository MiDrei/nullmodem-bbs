package areafix

import (
	"database/sql"
	"fmt"
	"strings"
)

// FileStore mirrors EchoStore exactly -- see its doc comment -- for
// file-echo (Filefix) subscriptions instead of echomail (Areafix)
// ones, backed by the parallel file_echo_subscriptions table.
type FileStore struct {
	db *sql.DB
}

// NewFileStore wraps an already-open database connection.
func NewFileStore(db *sql.DB) *FileStore { return &FileStore{db: db} }

// Request mirrors EchoStore.Request -- see its doc comment.
func (s *FileStore) Request(uplinkHost, areaTag string, direction Direction) error {
	_, err := s.db.Exec(
		`INSERT INTO file_echo_subscriptions (uplink_host, area_tag, direction, requested_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT (uplink_host, area_tag, direction)
		 DO UPDATE SET requested_at = CURRENT_TIMESTAMP`,
		uplinkHost, areaTag, string(direction),
	)
	if err != nil {
		return fmt.Errorf("areafix: request file-echo subscription: %w", err)
	}
	return nil
}

// Withdraw mirrors EchoStore.Withdraw -- see its doc comment.
func (s *FileStore) Withdraw(uplinkHost, areaTag string, direction Direction) error {
	_, err := s.db.Exec(
		`DELETE FROM file_echo_subscriptions WHERE uplink_host = ? AND area_tag = ? AND direction = ?`,
		uplinkHost, areaTag, string(direction),
	)
	if err != nil {
		return fmt.Errorf("areafix: withdraw file-echo subscription: %w", err)
	}
	return nil
}

// Grant mirrors EchoStore.Grant -- see its doc comment.
func (s *FileStore) Grant(uplinkHost, areaTag string) error {
	_, err := s.db.Exec(
		`INSERT INTO file_area_grants (uplink_host, area_tag, granted_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT (uplink_host, area_tag)
		 DO UPDATE SET granted_at = CURRENT_TIMESTAMP`,
		uplinkHost, areaTag,
	)
	if err != nil {
		return fmt.Errorf("areafix: grant file area: %w", err)
	}
	return nil
}

// Revoke mirrors EchoStore.Revoke -- see its doc comment.
func (s *FileStore) Revoke(uplinkHost, areaTag string) error {
	_, err := s.db.Exec(
		`DELETE FROM file_area_grants WHERE uplink_host = ? AND area_tag = ?`,
		uplinkHost, areaTag,
	)
	if err != nil {
		return fmt.Errorf("areafix: revoke file area grant: %w", err)
	}
	return nil
}

// IsGranted mirrors EchoStore.IsGranted -- see its doc comment.
func (s *FileStore) IsGranted(uplinkHost, areaTag string) (bool, error) {
	var exists int
	err := s.db.QueryRow(
		`SELECT 1 FROM file_area_grants WHERE uplink_host = ? AND area_tag = ?`,
		uplinkHost, areaTag,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("areafix: check file area grant: %w", err)
	}
	return true, nil
}

// GrantedTags mirrors EchoStore.GrantedTags -- see its doc comment.
func (s *FileStore) GrantedTags(uplinkHost string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT area_tag FROM file_area_grants WHERE uplink_host = ?`, uplinkHost)
	if err != nil {
		return nil, fmt.Errorf("areafix: list file area grants: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("areafix: scan file area grant: %w", err)
		}
		out[strings.ToUpper(tag)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("areafix: list file area grants: %w", err)
	}
	return out, nil
}

// ListForUplink mirrors EchoStore.ListForUplink -- see its doc
// comment.
func (s *FileStore) ListForUplink(uplinkHost string, direction Direction) ([]Subscription, error) {
	rows, err := s.db.Query(
		`SELECT id, uplink_host, area_tag, direction, requested_at FROM file_echo_subscriptions
		 WHERE uplink_host = ? AND direction = ? ORDER BY requested_at DESC`,
		uplinkHost, string(direction),
	)
	if err != nil {
		return nil, fmt.Errorf("areafix: list file-echo subscriptions: %w", err)
	}
	defer rows.Close()

	var out []Subscription
	for rows.Next() {
		var sub Subscription
		var direction string
		if err := rows.Scan(&sub.ID, &sub.UplinkHost, &sub.AreaTag, &direction, &sub.RequestedAt); err != nil {
			return nil, fmt.Errorf("areafix: scan file-echo subscription: %w", err)
		}
		sub.Direction = Direction(direction)
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("areafix: list file-echo subscriptions: %w", err)
	}
	return out, nil
}
