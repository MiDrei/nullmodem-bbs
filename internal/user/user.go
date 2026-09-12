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

// User is one BBS account.
type User struct {
	ID            int64
	Username      string
	SecurityLevel int
	CreatedAt     time.Time
	LastLoginAt   sql.NullTime
	TotalCalls    int
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
	}

	res, err := tx.Exec(
		`INSERT INTO users (username, password_hash, security_level) VALUES (?, ?, ?)`,
		username, string(hash), securityLevel,
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
		`SELECT id, username, password_hash, security_level, created_at, last_login_at, total_calls
		 FROM users WHERE username = ?`, username,
	)
	if err := row.Scan(&u.ID, &u.Username, &hash, &u.SecurityLevel, &u.CreatedAt, &u.LastLoginAt, &u.TotalCalls); err != nil {
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

// ByID loads a single user by primary key.
func (s *Store) ByID(id int64) (*User, error) {
	return s.scanOne(s.db.QueryRow(
		`SELECT id, username, security_level, created_at, last_login_at, total_calls
		 FROM users WHERE id = ?`, id,
	))
}

// ByUsername loads a single user by handle (matched case-insensitively).
func (s *Store) ByUsername(username string) (*User, error) {
	return s.scanOne(s.db.QueryRow(
		`SELECT id, username, security_level, created_at, last_login_at, total_calls
		 FROM users WHERE username = ?`, username,
	))
}

func (s *Store) scanOne(row *sql.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.SecurityLevel, &u.CreatedAt, &u.LastLoginAt, &u.TotalCalls); err != nil {
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
	rows, err := s.db.Query(
		`SELECT id, username, security_level, created_at, last_login_at, total_calls
		 FROM users ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("user: list all: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.SecurityLevel, &u.CreatedAt, &u.LastLoginAt, &u.TotalCalls); err != nil {
			return nil, fmt.Errorf("user: scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user: list all: %w", err)
	}
	return users, nil
}

// SetSecurityLevel updates a user's SL (0-255). It does not validate
// the range itself; callers (e.g. the future web admin API) are
// expected to clamp/validate user input before calling this.
func (s *Store) SetSecurityLevel(id int64, level int) error {
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
