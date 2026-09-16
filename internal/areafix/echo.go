// Package areafix implements FTS-compatible Areafix/Filefix
// subscription management: composing the netmail commands that
// request (or drop) an echo/file-echo area from one of our own
// uplinks' robot ("+TAG"/"-TAG", first body line the uplink's
// configured password -- see config.BinkpUplink.AreafixPassword), and
// tracking what's been requested so the web admin UI can show current
// state.
//
// This is the outbound half only: composing our own requests to an
// uplink. Running our own Areafix/Filefix robot for a downlink's
// inbound requests, and forwarding echo/file-echo traffic to
// subscribed downlinks (hub distribution), are both still TODO.
package areafix

import (
	"database/sql"
	"fmt"
	"time"
)

// Direction distinguishes a subscription WE requested from one of our
// own uplinks ("Outbound": we're their downlink for that area) from
// one a downlink requested from us ("Inbound": they're our downlink,
// once an Areafix/Filefix robot exists to receive that request).
type Direction string

const (
	// Outbound is an area we asked an uplink's own Areafix/Filefix
	// robot for.
	Outbound Direction = "outbound"
	// Inbound is an area a downlink asked our own robot for.
	// Recorded for when that robot exists; nothing writes it yet.
	Inbound Direction = "inbound"
)

// Subscription is one row of recorded subscription state.
type Subscription struct {
	ID          int64
	UplinkHost  string
	AreaTag     string
	Direction   Direction
	RequestedAt time.Time
}

// EchoStore persists echomail Areafix subscription state in the
// shared SQLite database (the echo_subscriptions table). AreaTag is a
// bare string, not a message_areas foreign key: the whole point of an
// outbound request is often a tag we don't have a local Area for yet
// -- one gets auto-created (as Pending) once the first echomail
// actually arrives under that tag, the same way any other
// unrecognized inbound AREA kludge does.
type EchoStore struct {
	db *sql.DB
}

// NewEchoStore wraps an already-open database connection.
func NewEchoStore(db *sql.DB) *EchoStore { return &EchoStore{db: db} }

// Request records that direction's side asked for areaTag from/at
// uplinkHost -- an upsert, so re-requesting the same (uplink, tag,
// direction) just refreshes RequestedAt instead of erroring on the
// table's UNIQUE constraint.
func (s *EchoStore) Request(uplinkHost, areaTag string, direction Direction) error {
	_, err := s.db.Exec(
		`INSERT INTO echo_subscriptions (uplink_host, area_tag, direction, requested_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT (uplink_host, area_tag, direction)
		 DO UPDATE SET requested_at = CURRENT_TIMESTAMP`,
		uplinkHost, areaTag, string(direction),
	)
	if err != nil {
		return fmt.Errorf("areafix: request echo subscription: %w", err)
	}
	return nil
}

// Withdraw removes a subscription record (e.g. after sending a
// "-TAG" unsubscribe command) -- not being present isn't an error.
func (s *EchoStore) Withdraw(uplinkHost, areaTag string, direction Direction) error {
	_, err := s.db.Exec(
		`DELETE FROM echo_subscriptions WHERE uplink_host = ? AND area_tag = ? AND direction = ?`,
		uplinkHost, areaTag, string(direction),
	)
	if err != nil {
		return fmt.Errorf("areafix: withdraw echo subscription: %w", err)
	}
	return nil
}

// ListForUplink returns every subscription recorded for uplinkHost in
// the given direction, most recently requested first.
func (s *EchoStore) ListForUplink(uplinkHost string, direction Direction) ([]Subscription, error) {
	rows, err := s.db.Query(
		`SELECT id, uplink_host, area_tag, direction, requested_at FROM echo_subscriptions
		 WHERE uplink_host = ? AND direction = ? ORDER BY requested_at DESC`,
		uplinkHost, string(direction),
	)
	if err != nil {
		return nil, fmt.Errorf("areafix: list echo subscriptions: %w", err)
	}
	defer rows.Close()

	var out []Subscription
	for rows.Next() {
		var sub Subscription
		var direction string
		if err := rows.Scan(&sub.ID, &sub.UplinkHost, &sub.AreaTag, &direction, &sub.RequestedAt); err != nil {
			return nil, fmt.Errorf("areafix: scan echo subscription: %w", err)
		}
		sub.Direction = Direction(direction)
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("areafix: list echo subscriptions: %w", err)
	}
	return out, nil
}
