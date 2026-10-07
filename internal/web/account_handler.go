package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

// The signed-in sysop's own two-factor login (Admin -> Security), and
// helping other accounts: a new password, or two-factor turned off for
// one whose phone is gone.

type codeRequest struct {
	Code string `json:"code"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) (*user.User, bool) {
	claims, _ := claimsFromContext(r.Context())
	u, err := s.Users.ByID(claims.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return nil, false
	}
	return u, true
}

func (s *Server) writeTOTPStatus(w http.ResponseWriter, u *user.User) {
	left, _ := s.Users.RecoveryCodesLeft(u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": u.TwoFactor, "recovery_codes_left": left})
}

// handleGetTOTP: GET /api/account/totp.
func (s *Server) handleGetTOTP(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.me(w, r); ok {
		s.writeTOTPStatus(w, u)
	}
}

// handleStartTOTP: POST /api/account/totp/start -- a new secret to scan.
func (s *Server) handleStartTOTP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.me(w, r)
	if !ok {
		return
	}
	if u.TwoFactor {
		writeError(w, http.StatusConflict, "two-factor login is on already -- turn it off first")
		return
	}
	issuer := "NullModem BBS"
	if c, err := s.loadBBSConfig(); err == nil && c.BBS.Name != "" {
		issuer = c.BBS.Name
	}
	setup, err := s.Users.StartTOTP(u.ID, issuer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set it up")
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

// handleConfirmTOTP: POST /api/account/totp/confirm {code} -- on, and
// the recovery codes (shown this once).
func (s *Server) handleConfirmTOTP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.me(w, r)
	if !ok {
		return
	}
	var req codeRequest
	json.NewDecoder(r.Body).Decode(&req)
	codes, err := s.Users.ConfirmTOTP(u.ID, req.Code, time.Now())
	switch {
	case errors.Is(err, user.ErrBadCode):
		writeError(w, http.StatusBadRequest, "that code doesn't match -- check the time on your phone and try the next one")
		return
	case errors.Is(err, user.ErrNoTOTP):
		writeError(w, http.StatusConflict, "start the setup first")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not turn it on")
		return
	}
	s.logInfo("%s turned on two-factor login", u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// handleDisableTOTP: POST /api/account/totp/disable {code}.
func (s *Server) handleDisableTOTP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.me(w, r)
	if !ok {
		return
	}
	var req codeRequest
	json.NewDecoder(r.Body).Decode(&req)
	if err := s.Users.VerifyTOTP(u.ID, req.Code, time.Now()); err != nil {
		writeError(w, http.StatusBadRequest, "wrong code")
		return
	}
	if s.adminRequiresTOTP() {
		writeError(w, http.StatusConflict, "this BBS requires two-factor login for the admin -- turn that off first, or you'd lock yourself out")
		return
	}
	if err := s.Users.DisableTOTP(u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not turn it off")
		return
	}
	s.logInfo("%s turned off two-factor login", u.Username)
	u.TwoFactor = false
	s.writeTOTPStatus(w, u)
}

// handleNewRecoveryCodes: POST /api/account/totp/recovery {code}.
func (s *Server) handleNewRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	u, ok := s.me(w, r)
	if !ok {
		return
	}
	var req codeRequest
	json.NewDecoder(r.Body).Decode(&req)
	if err := s.Users.VerifyTOTP(u.ID, req.Code, time.Now()); err != nil {
		writeError(w, http.StatusBadRequest, "wrong code")
		return
	}
	codes, err := s.Users.NewRecoveryCodes(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not make new ones")
		return
	}
	s.logInfo("%s made new two-factor recovery codes", u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// handleSetUserPassword: PUT /api/users/{id}/password {password}.
func (s *Server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	switch err := s.Users.SetPassword(id, req.Password); {
	case errors.Is(err, user.ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, user.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such user")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not set the password")
		return
	}
	target, _ := s.Users.ByID(id)
	if claims, ok := claimsFromContext(r.Context()); ok && target != nil {
		s.logInfo("%s set a new password for %s", claims.Subject, target.Username)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleResetUserTOTP: DELETE /api/users/{id}/totp -- for another sysop
// who lost their authenticator and recovery codes.
func (s *Server) handleResetUserTOTP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	claims, _ := claimsFromContext(r.Context())
	if id == claims.UserID {
		writeError(w, http.StatusBadRequest, "turn off your own under Security, with a code")
		return
	}
	target, err := s.Users.ByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such user")
		return
	}
	if err := s.Users.DisableTOTP(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not reset it")
		return
	}
	s.logWarn("%s turned off two-factor login for %s", claims.Subject, target.Username)
	w.WriteHeader(http.StatusNoContent)
}
