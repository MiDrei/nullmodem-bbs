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
	"strings"
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

// Grant records that uplinkHost (a downlink) is permitted to request
// areaTag via the inbound Areafix robot (internal/tosser's
// handleAreafixRequest) -- see echo_area_grants' schema comment for
// why this exists separately from Request/the 'inbound' Direction: a
// downlink with no grants can request nothing at all, even with the
// correct password, until the sysop grants specific areas here. An
// upsert, so granting an already-granted area just refreshes
// GrantedAt instead of erroring on the table's UNIQUE constraint.
func (s *EchoStore) Grant(uplinkHost, areaTag string) error {
	_, err := s.db.Exec(
		`INSERT INTO echo_area_grants (uplink_host, area_tag, granted_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT (uplink_host, area_tag)
		 DO UPDATE SET granted_at = CURRENT_TIMESTAMP`,
		uplinkHost, areaTag,
	)
	if err != nil {
		return fmt.Errorf("areafix: grant echo area: %w", err)
	}
	return nil
}

// Revoke removes a previously granted area -- not being present isn't
// an error.
func (s *EchoStore) Revoke(uplinkHost, areaTag string) error {
	_, err := s.db.Exec(
		`DELETE FROM echo_area_grants WHERE uplink_host = ? AND area_tag = ?`,
		uplinkHost, areaTag,
	)
	if err != nil {
		return fmt.Errorf("areafix: revoke echo area grant: %w", err)
	}
	return nil
}

// IsGranted reports whether uplinkHost has been granted areaTag.
func (s *EchoStore) IsGranted(uplinkHost, areaTag string) (bool, error) {
	var exists int
	err := s.db.QueryRow(
		`SELECT 1 FROM echo_area_grants WHERE uplink_host = ? AND area_tag = ?`,
		uplinkHost, areaTag,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("areafix: check echo area grant: %w", err)
	}
	return true, nil
}

// GrantedTags returns every area tag granted to uplinkHost, for
// filtering a %LIST catalog down to only what a downlink is actually
// permitted to see/request (see internal/tosser's areaCatalog).
func (s *EchoStore) GrantedTags(uplinkHost string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT area_tag FROM echo_area_grants WHERE uplink_host = ?`, uplinkHost)
	if err != nil {
		return nil, fmt.Errorf("areafix: list echo area grants: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("areafix: scan echo area grant: %w", err)
		}
		out[strings.ToUpper(tag)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("areafix: list echo area grants: %w", err)
	}
	return out, nil
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

// Grants returns uplinkHost's granted areas as inbound subscriptions,
// RequestedAt being when each was granted -- for a point that is the
// sysop's own reader (internal/tosser's points.go), where ticking an
// area in the web admin is the subscription.
func (s *EchoStore) Grants(uplinkHost string) ([]Subscription, error) {
	rows, err := s.db.Query(`SELECT id, uplink_host, area_tag, granted_at FROM echo_area_grants WHERE uplink_host = ?`, uplinkHost)
	if err != nil {
		return nil, fmt.Errorf("areafix: list echo area grants: %w", err)
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		sub := Subscription{Direction: Inbound}
		if err := rows.Scan(&sub.ID, &sub.UplinkHost, &sub.AreaTag, &sub.RequestedAt); err != nil {
			return nil, fmt.Errorf("areafix: scan echo area grant: %w", err)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
