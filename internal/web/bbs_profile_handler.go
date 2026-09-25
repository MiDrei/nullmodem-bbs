package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

// profileDTO is the portal's /profile page -- the same account
// overview and settings the Telnet/SSH profile (internal/bbs's
// showProfile) offers.
type profileDTO struct {
	Username      string    `json:"username"`
	RealName      string    `json:"real_name"`
	SecurityLevel int       `json:"security_level"`
	TotalCalls    int       `json:"total_calls"`
	CreatedAt     time.Time `json:"created_at"`
	// Timezone is an IANA name, or "" if not set -- the portal then
	// shows times in the browser's own zone, Telnet/SSH in UTC.
	Timezone string `json:"timezone"`
}

func toProfileDTO(u *user.User) profileDTO {
	return profileDTO{
		Username:      u.Username,
		RealName:      u.RealName,
		SecurityLevel: u.SecurityLevel,
		TotalCalls:    u.TotalCalls,
		CreatedAt:     u.CreatedAt,
		Timezone:      u.Timezone,
	}
}

func (s *Server) handleGetBBSProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	u, err := s.Users.ByID(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}
	writeJSON(w, http.StatusOK, toProfileDTO(u))
}

// handleUpdateBBSProfile changes the caller's real name and/or time
// zone; a field left out of the body is left unchanged. Both are
// validated before either is written, by the same rules the Telnet/SSH
// profile applies (user.ValidateRealName, user.ValidateTimezone).
func (s *Server) handleUpdateBBSProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	var req struct {
		RealName *string `json:"real_name"`
		Timezone *string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var realName, timezone string
	if req.RealName != nil {
		realName = strings.TrimSpace(*req.RealName)
		switch err := user.ValidateRealName(realName); {
		case errors.Is(err, user.ErrRealNameRequired):
			writeError(w, http.StatusBadRequest, "real name is required")
			return
		case errors.Is(err, user.ErrRealNameReserved):
			writeError(w, http.StatusBadRequest, "that name is reserved")
			return
		}
	}
	if req.Timezone != nil {
		timezone = strings.TrimSpace(*req.Timezone)
		if err := user.ValidateTimezone(timezone); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown time zone %q", timezone))
			return
		}
	}

	if req.RealName != nil {
		if err := s.Users.SetRealName(claims.UserID, realName); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save real name")
			return
		}
		s.logInfo("%s changed their real name via the BBS portal", claims.Subject)
	}
	if req.Timezone != nil {
		if err := s.Users.SetTimezone(claims.UserID, timezone); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save time zone")
			return
		}
		s.logInfo("%s set their time zone to %q via the BBS portal", claims.Subject, timezone)
	}

	u, err := s.Users.ByID(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}
	writeJSON(w, http.StatusOK, toProfileDTO(u))
}

// handleChangeBBSPassword verifies the caller's current password and
// sets a new one. A wrong current password is 400, not 401: the
// caller's session itself is still valid, and the portal treats 401 as
// "log out".
func (s *Server) handleChangeBBSPassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch err := s.Users.ChangePassword(claims.UserID, req.CurrentPassword, req.NewPassword); {
	case errors.Is(err, user.ErrInvalidCredentials):
		s.logWarn("%s: failed password change via the BBS portal (wrong current password)", claims.Subject)
		writeError(w, http.StatusBadRequest, "current password is incorrect")
		return
	case errors.Is(err, user.ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("password must be at least %d characters", user.MinPasswordLength))
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not change password")
		return
	}
	s.logInfo("%s changed their password via the BBS portal", claims.Subject)
	w.WriteHeader(http.StatusNoContent)
}
