package areafix

import (
	"database/sql"
	"fmt"
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
