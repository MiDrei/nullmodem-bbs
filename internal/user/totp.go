package user

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// Two-factor login with a time-based one-time code (TOTP, RFC 6238)
// from an authenticator app: set up with StartTOTP (a QR code to scan)
// and ConfirmTOTP (the first code, which also gives the recovery
// codes), checked with VerifyTOTP. A code is good for its 30 seconds
// and the steps either side of it (clocks drift), and only once.

// ErrNoTOTP: the account has no two-factor login (or nothing pending).
var ErrNoTOTP = errors.New("user: no two-factor login set up")

// ErrBadCode is a wrong (or already used) code.
var ErrBadCode = errors.New("user: wrong code")

// recoveryCodes is how many one-time recovery codes an account gets.
const recoveryCodes = 8

// TOTPSetup is what the authenticator app needs.
type TOTPSetup struct {
	Secret string `json:"secret"`
	URL    string `json:"url"`
	// QR is the otpauth URL as a PNG data URL, to scan.
	QR string `json:"qr"`
}

// HasTOTP reports whether the account logs in with a second factor.
func (s *Store) HasTOTP(id int64) (bool, error) {
	var secret string
	if err := s.db.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, id).Scan(&secret); err != nil {
		return false, fmt.Errorf("user: totp: %w", err)
	}
	return secret != "", nil
}

// StartTOTP makes a new secret for id, pending until ConfirmTOTP; issuer
// is the BBS's name, shown in the app.
func (s *Store) StartTOTP(id int64, issuer string) (TOTPSetup, error) {
	u, err := s.ByID(id)
	if err != nil {
		return TOTPSetup{}, err
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: u.Username})
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("user: totp: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE users SET totp_pending = ? WHERE id = ?`, key.Secret(), id); err != nil {
		return TOTPSetup{}, fmt.Errorf("user: totp: %w", err)
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("user: totp: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return TOTPSetup{}, fmt.Errorf("user: totp: %w", err)
	}
	return TOTPSetup{Secret: key.Secret(), URL: key.URL(),
		QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
}

// ConfirmTOTP turns the pending secret on, given a code from it, and
// returns fresh recovery codes (shown once; only their hashes stay).
func (s *Store) ConfirmTOTP(id int64, code string, now time.Time) ([]string, error) {
	var pending string
	if err := s.db.QueryRow(`SELECT totp_pending FROM users WHERE id = ?`, id).Scan(&pending); err != nil {
		return nil, fmt.Errorf("user: totp: %w", err)
	}
	if pending == "" {
		return nil, ErrNoTOTP
	}
	step, ok := matchCode(pending, code, now, 0)
	if !ok {
		return nil, ErrBadCode
	}
	if _, err := s.db.Exec(`UPDATE users SET totp_secret = totp_pending, totp_pending = '', totp_last = ? WHERE id = ?`, step, id); err != nil {
		return nil, fmt.Errorf("user: totp: %w", err)
	}
	return s.NewRecoveryCodes(id)
}

// NewRecoveryCodes replaces id's recovery codes.
func (s *Store) NewRecoveryCodes(id int64) ([]string, error) {
	codes := make([]string, recoveryCodes)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("user: totp: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM totp_recovery WHERE user_id = ?`, id); err != nil {
		return nil, fmt.Errorf("user: totp: %w", err)
	}
	for i := range codes {
		codes[i] = recoveryCode()
		hash, err := bcrypt.GenerateFromPassword([]byte(codes[i]), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("user: totp: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO totp_recovery (user_id, hash) VALUES (?, ?)`, id, string(hash)); err != nil {
			return nil, fmt.Errorf("user: totp: %w", err)
		}
	}
	return codes, tx.Commit()
}

// RecoveryCodesLeft counts id's unused recovery codes.
func (s *Store) RecoveryCodesLeft(id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM totp_recovery WHERE user_id = ? AND used = 0`, id).Scan(&n)
	return n, err
}

// recoveryCode is "xxxx-xxxx" from an alphabet without look-alikes.
func recoveryCode() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 8)
	rand.Read(b)
	var out strings.Builder
	for i, c := range b {
		if i == 4 {
			out.WriteByte('-')
		}
		out.WriteByte(alphabet[int(c)%len(alphabet)])
	}
	return out.String()
}

// VerifyTOTP checks a code from id's authenticator -- or one of its
// recovery codes, used up by it.
func (s *Store) VerifyTOTP(id int64, code string, now time.Time) error {
	var secret string
	var last int64
	if err := s.db.QueryRow(`SELECT totp_secret, totp_last FROM users WHERE id = ?`, id).Scan(&secret, &last); err != nil {
		return fmt.Errorf("user: totp: %w", err)
	}
	if secret == "" {
		return ErrNoTOTP
	}
	code = strings.ToLower(strings.TrimSpace(code))
	if step, ok := matchCode(secret, code, now, last); ok {
		// A step only once: a code seen over someone's shoulder is spent.
		res, err := s.db.Exec(`UPDATE users SET totp_last = ? WHERE id = ? AND totp_last < ?`, step, id, step)
		if err != nil {
			return fmt.Errorf("user: totp: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return nil
		}
		return ErrBadCode
	}
	if len(code) == 9 && code[4] == '-' {
		rows, err := s.db.Query(`SELECT rowid, hash FROM totp_recovery WHERE user_id = ? AND used = 0`, id)
		if err != nil {
			return fmt.Errorf("user: totp: %w", err)
		}
		var hit int64
		for rows.Next() {
			var rowid int64
			var hash string
			rows.Scan(&rowid, &hash)
			if hit == 0 && bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil {
				hit = rowid
			}
		}
		rows.Close()
		if hit != 0 {
			if _, err := s.db.Exec(`UPDATE totp_recovery SET used = 1 WHERE rowid = ?`, hit); err != nil {
				return fmt.Errorf("user: totp: %w", err)
			}
			return nil
		}
	}
	return ErrBadCode
}

// matchCode checks code against secret for the current 30-second step
// and the ones either side, never at or before last; it returns the
// matching step.
func matchCode(secret, code string, now time.Time, last int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	step := now.Unix() / 30
	for _, d := range []int64{0, -1, 1} {
		st := step + d
		if st <= last {
			continue
		}
		want, err := totp.GenerateCodeCustom(secret, time.Unix(st*30, 0), totp.ValidateOpts{
			Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
		})
		if err == nil && want == code {
			return st, true
		}
	}
	return 0, false
}

// DisableTOTP turns id's two-factor login off (by the account itself,
// with a code, or by another sysop for one whose phone is gone).
func (s *Store) DisableTOTP(id int64) error {
	if _, err := s.db.Exec(`UPDATE users SET totp_secret = '', totp_pending = '', totp_last = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("user: totp: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM totp_recovery WHERE user_id = ?`, id); err != nil {
		return fmt.Errorf("user: totp: %w", err)
	}
	return nil
}

// SetPassword sets a new password for id -- the sysop helping a caller
// who forgot theirs.
func (s *Store) SetPassword(id int64, password string) error {
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user: set password: %w", err)
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
	if err != nil {
		return fmt.Errorf("user: set password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
