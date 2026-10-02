// Package user implements the BBS user account store: registration,
// password authentication, and the SL 0-255 security level assigned
// to each account.
package user

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	// Embedded zone database: the runtime image (debian:trixie-slim)
	// isn't guaranteed to ship /usr/share/zoneinfo, and a missing zone
	// would silently fall back to UTC (see Location).
	_ "time/tzdata"

	"golang.org/x/crypto/bcrypt"
)

// Named security levels for the handful of tiers the BBS itself
// depends on. Everything in between is free for a sysop to define via
// the (future) web SL permission matrix; the column simply holds
// 0-255.
const (
	SLNewUser = 10
	SLSysop   = 255
)

// ErrUsernameTaken is returned by Register when the handle already
// exists (case-insensitively).
var ErrUsernameTaken = errors.New("user: username already taken")

// ErrInvalidCredentials is returned by Authenticate when the username
// is unknown or the password does not match.
var ErrInvalidCredentials = errors.New("user: invalid username or password")

// ErrNotFound is returned by ByID and ByUsername when no matching
// account exists.
var ErrNotFound = errors.New("user: not found")

// MinPasswordLength is the shortest password accepted at registration
// and when changing it (see ChangePassword).
const MinPasswordLength = 6

// ErrPasswordTooShort is returned by ChangePassword for a new password
// under MinPasswordLength.
var ErrPasswordTooShort = fmt.Errorf("user: password must be at least %d characters", MinPasswordLength)

// ErrRealNameRequired and ErrRealNameReserved are returned by
// ValidateRealName.
var (
	ErrRealNameRequired = errors.New("user: real name is required")
	ErrRealNameReserved = errors.New("user: that name is reserved")
)

// ErrInvalidTimezone is returned by SetTimezone for a name that isn't
// a known IANA zone.
var ErrInvalidTimezone = errors.New("user: unknown time zone")

// ErrLastSysop is returned by SetSecurityLevel when lowering an
// account below SLSysop would leave no account at sysop level at all,
// locking everyone out of every sysop-only feature (web admin login,
// telnet sysop menu) with no built-in way back in.
var ErrLastSysop = errors.New("user: cannot demote the last sysop-level account")

// User is one BBS account.
type User struct {
	ID       int64
	Username string
	// RealName is required at Telnet/SSH registration (internal/bbs's
	// registerNew/promptRealName -- many FTN networks reject a
	// handle-only participant) and editable by a sysop afterward (see
	// SetRealName). May still be "" for an account that existed before
	// this field did.
	RealName      string
	SecurityLevel int
	CreatedAt     time.Time
	LastLoginAt   sql.NullTime
	TotalCalls    int
	// Timezone is the IANA zone name (e.g. "Europe/Zurich") the caller
	// chose in their profile, or "" if never set -- see Location.
	Timezone string
	// QWKRouting puts echomail's SEEN-BY/PATH lines into this user's
	// QWK packets, for an offline reader that hides them and can quote
	// them (NullModem Reader). Off by default: most readers show them.
	QWKRouting bool
	// Place is where the caller is, as they wrote it ("Neunkirch,
	// Switzerland") -- optional, shown on the InterBBS last callers
	// list. (Not "Location": that's the time zone, see Location().)
	Place string
	// Validated is false for an account still waiting for the
	// sysop's approval (see RegisterNew, Approve): it may read and
	// write to the sysop, not post or use doors.
	Validated bool
	// LineEditor: messages are written line by line (/S to save), not
	// in the full-screen editor -- for terminals without cursor keys.
	LineEditor bool
	// TwoFactor: the account logs into the admin (and the Telnet sysop
	// menu) with a code from an authenticator app too (totp.go).
	TwoFactor bool
}

// Location returns the zone to show this user's times in: their
// profile's Timezone, or UTC if it's unset (or, defensively, no longer
// loadable). The web portal treats "" differently -- it falls back to
// the browser's own zone, since it has one to offer.
func (u *User) Location() *time.Location {
	if u.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ValidateTimezone reports whether name is acceptable for SetTimezone:
// "" (not set) or a loadable IANA zone name. "Local" is rejected --
// it would mean the server's own zone, not something a caller chose.
func ValidateTimezone(name string) error {
	if name == "" {
		return nil
	}
	if name == "Local" {
		return ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(name); err != nil {
		return ErrInvalidTimezone
	}
	return nil
}

// ValidateRealName checks a real name a caller wants to set, the same
// rules registration applies: required, and not a reserved system/staff
// role (see IsRestrictedRealName). Callers pass it already trimmed.
func ValidateRealName(realName string) error {
	if realName == "" {
		return ErrRealNameRequired
	}
	if IsRestrictedRealName(realName) {
		return ErrRealNameReserved
	}
	return nil
}

// userColumns is the column list every single-/multi-row user query
// selects, in scanUser's order.
const userColumns = `id, username, real_name, security_level, created_at, last_login_at, total_calls, timezone, qwk_routing, location, validated, line_editor, totp_secret <> ''`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner, u *User) error {
	return row.Scan(&u.ID, &u.Username, &u.RealName, &u.SecurityLevel, &u.CreatedAt, &u.LastLoginAt, &u.TotalCalls, &u.Timezone, &u.QWKRouting, &u.Place, &u.Validated, &u.LineEditor, &u.TwoFactor)
}

// Store persists User accounts in the shared SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore wraps an already-opened database handle (see internal/db).
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Register creates a new account with the given username and
// password, hashed with bcrypt, at the given initial security level.
// The username lookup is case-insensitive (enforced by the schema's
// COLLATE NOCASE unique index).
//
// The very first account ever registered becomes sysop (SLSysop)
// regardless of securityLevel, following BBS convention, so a fresh
// install always has a way into the web admin UI. The count check and
// insert run in one transaction so two concurrent first registrations
// can't both claim it.
func (s *Store) Register(username, password string, securityLevel int) (*User, error) {
	return s.RegisterNew(username, password, securityLevel, false)
}

// RegisterNew is Register for a caller signing up: with pending, the
// account waits for the sysop's approval (Validated false) at
// securityLevel -- the very first account, the sysop, never does.
func (s *Store) RegisterNew(username, password string, securityLevel int, pending bool) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("user: hash password: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("user: begin transaction: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&count); err != nil {
		return nil, fmt.Errorf("user: count users: %w", err)
	}
	if count == 0 {
		securityLevel = SLSysop
		pending = false
	}

	res, err := tx.Exec(
		`INSERT INTO users (username, password_hash, security_level, validated) VALUES (?, ?, ?, ?)`,
		username, string(hash), securityLevel, !pending,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("user: insert %s: %w", username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("user: last insert id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("user: commit: %w", err)
	}
	return s.ByID(id)
}

// Exists reports whether an account with the given username (matched
// case-insensitively) already exists.
func (s *Store) Exists(username string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE username = ?`, username).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("user: exists %s: %w", username, err)
	}
	return n > 0, nil
}

// Authenticate verifies a username/password pair and, on success,
// bumps last_login_at and total_calls.
func (s *Store) Authenticate(username, password string) (*User, error) {
	var (
		u    User
		hash string
	)
	row := s.db.QueryRow(
		`SELECT id, username, password_hash, real_name, security_level, created_at, last_login_at, total_calls
		 FROM users WHERE username = ?`, username,
	)
	if err := row.Scan(&u.ID, &u.Username, &hash, &u.RealName, &u.SecurityLevel, &u.CreatedAt, &u.LastLoginAt, &u.TotalCalls); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("user: lookup %s: %w", username, err)
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}

	if _, err := s.db.Exec(
		`UPDATE users SET last_login_at = CURRENT_TIMESTAMP, total_calls = total_calls + 1 WHERE id = ?`,
		u.ID,
	); err != nil {
		return nil, fmt.Errorf("user: record login for %s: %w", username, err)
	}

	return s.ByID(u.ID)
}

// Pending returns the accounts waiting for approval, oldest first.
func (s *Store) Pending() ([]User, error) {
	rows, err := s.db.Query(`SELECT ` + userColumns + ` FROM users WHERE validated = 0 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("user: pending: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := scanUser(rows, &u); err != nil {
			return nil, fmt.Errorf("user: pending: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ErrNotPending: only an account still waiting for approval may be
// deleted this way.
var ErrNotPending = errors.New("user: account is not waiting for approval")

// DeletePending deletes an account still waiting for approval -- one
// the sysop turned down. Netmail it wrote keeps its sender's name.
func (s *Store) DeletePending(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("user: delete %d: %w", id, err)
	}
	defer tx.Rollback()
	var validated bool
	if err := tx.QueryRow(`SELECT validated FROM users WHERE id = ?`, id).Scan(&validated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("user: delete %d: %w", id, err)
	}
	if validated {
		return ErrNotPending
	}
	for _, q := range []string{
		`UPDATE netmail_messages SET from_user_id = NULL WHERE from_user_id = ?`,
		`UPDATE messages SET from_user_id = NULL WHERE from_user_id = ?`,
		`UPDATE files SET uploaded_by = NULL WHERE uploaded_by = ?`,
		`DELETE FROM users WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return fmt.Errorf("user: delete %d: %w", id, err)
		}
	}
	return tx.Commit()
}

// Approve validates a pending account and raises it to securityLevel
// (never lowers it).
func (s *Store) Approve(id int64, securityLevel int) error {
	res, err := s.db.Exec(`UPDATE users SET validated = 1, security_level = MAX(security_level, ?) WHERE id = ?`, securityLevel, id)
	if err != nil {
		return fmt.Errorf("user: approve %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ByID loads a single user by primary key.
func (s *Store) ByID(id int64) (*User, error) {
	return s.scanOne(s.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// ByUsername loads a single user by handle (matched case-insensitively).
func (s *Store) ByUsername(username string) (*User, error) {
	return s.scanOne(s.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE username = ?`, username))
}

func (s *Store) scanOne(row *sql.Row) (*User, error) {
	var u User
	if err := scanUser(row, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user: load: %w", err)
	}
	return &u, nil
}

// ListAll returns every account, ordered by id (i.e. registration
// order), for the sysop user-list menu and admin API.
func (s *Store) ListAll() ([]User, error) {
	rows, err := s.db.Query(`SELECT ` + userColumns + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("user: list all: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := scanUser(rows, &u); err != nil {
			return nil, fmt.Errorf("user: scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user: list all: %w", err)
	}
	return users, nil
}

// SetRealName updates a user's optional real name -- set at
// registration (see internal/bbs's registerNew) or later corrected by
// a sysop. Not validated: an empty string is a legitimate choice for
// a caller who'd rather stay handle-only.
func (s *Store) SetRealName(id int64, realName string) error {
	if _, err := s.db.Exec(`UPDATE users SET real_name = ? WHERE id = ?`, realName, id); err != nil {
		return fmt.Errorf("user: set real name for id %d: %w", id, err)
	}
	return nil
}

// SetTimezone stores the zone a user's times are shown in: an IANA name
// (see ValidateTimezone) or "" to unset it. Returns ErrInvalidTimezone
// for anything else.
func (s *Store) SetTimezone(id int64, name string) error {
	if err := ValidateTimezone(name); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE users SET timezone = ? WHERE id = ?`, name, id); err != nil {
		return fmt.Errorf("user: set timezone for id %d: %w", id, err)
	}
	return nil
}

// SetQWKRouting turns the SEEN-BY/PATH lines in a user's QWK packets
// on or off (see User.QWKRouting).
// SetLineEditor sets whether the user writes messages with the line
// editor instead of the full-screen one.
func (s *Store) SetLineEditor(id int64, on bool) error {
	if _, err := s.db.Exec(`UPDATE users SET line_editor = ? WHERE id = ?`, on, id); err != nil {
		return fmt.Errorf("user: set line editor for id %d: %w", id, err)
	}
	return nil
}

func (s *Store) SetQWKRouting(id int64, on bool) error {
	if _, err := s.db.Exec(`UPDATE users SET qwk_routing = ? WHERE id = ?`, on, id); err != nil {
		return fmt.Errorf("user: set qwk routing for id %d: %w", id, err)
	}
	return nil
}

// ChangePassword replaces a user's password after verifying their
// current one: ErrInvalidCredentials if current doesn't match,
// ErrPasswordTooShort if next is under MinPasswordLength.
func (s *Store) ChangePassword(id int64, current, next string) error {
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("user: load password for id %d: %w", id, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	if len(next) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user: hash password: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(newHash), id); err != nil {
		return fmt.Errorf("user: set password for id %d: %w", id, err)
	}
	return nil
}

// Count returns the total number of registered accounts, for the web
// admin dashboard.
func (s *Store) Count() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("user: count: %w", err)
	}
	return n, nil
}

// SetSecurityLevel updates a user's SL (0-255). It does not validate
// the range itself; callers (e.g. the web admin API) are expected to
// clamp/validate user input before calling this. It does refuse (with
// ErrLastSysop) to take the last sysop-level account below SLSysop --
// see ErrLastSysop.
func (s *Store) SetSecurityLevel(id int64, level int) error {
	if level < SLSysop {
		current, err := s.ByID(id)
		if err != nil {
			return fmt.Errorf("user: set security level for id %d: %w", id, err)
		}
		if current.SecurityLevel >= SLSysop {
			var otherSysops int
			if err := s.db.QueryRow(
				`SELECT COUNT(1) FROM users WHERE security_level >= ? AND id != ?`, SLSysop, id,
			).Scan(&otherSysops); err != nil {
				return fmt.Errorf("user: check remaining sysops: %w", err)
			}
			if otherSysops == 0 {
				return ErrLastSysop
			}
		}
	}

	if _, err := s.db.Exec(`UPDATE users SET security_level = ? WHERE id = ?`, level, id); err != nil {
		return fmt.Errorf("user: set security level for id %d: %w", id, err)
	}
	return nil
}

func isUniqueConstraintErr(err error) bool {
	// modernc.org/sqlite wraps the underlying SQLite error message
	// rather than exposing a typed sentinel, so a substring match is
	// the recognized way to detect it.
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

// FirstSysop is the oldest account with sysop access -- the one a
// message the system itself posts (an InterBBS record) is filed under.
func (s *Store) FirstSysop() (*User, error) {
	return s.scanOne(s.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE security_level >= ? ORDER BY id LIMIT 1`, SLSysop))
}

// MaxPlaceLen is how long User.Place may be.
const MaxPlaceLen = 40

// CleanPlace trims a place as typed and reports whether it fits
// (at most MaxPlaceLen characters, no control characters).
func CleanPlace(place string) (string, bool) {
	place = strings.Join(strings.Fields(place), " ")
	if len([]rune(place)) > MaxPlaceLen {
		return place, false
	}
	for _, r := range place {
		if r < 0x20 || r == 0x7f {
			return place, false
		}
	}
	return place, true
}

// SetPlace sets a user's place (see User.Place); "" clears it.
func (s *Store) SetPlace(id int64, place string) error {
	if _, err := s.db.Exec(`UPDATE users SET location = ? WHERE id = ?`, place, id); err != nil {
		return fmt.Errorf("user: set place: %w", err)
	}
	return nil
}

// DB is the store's database, for packages that keep state beside it
// (tests wiring up internal/guard).
func (s *Store) DB() *sql.DB { return s.db }
