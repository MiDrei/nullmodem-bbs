package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

type userDTO struct {
	ID            int64   `json:"id"`
	Username      string  `json:"username"`
	RealName      string  `json:"real_name"`
	SecurityLevel int     `json:"security_level"`
	CreatedAt     string  `json:"created_at"`
	LastLoginAt   *string `json:"last_login_at"`
	TotalCalls    int     `json:"total_calls"`
}

func toUserDTO(u user.User) userDTO {
	dto := userDTO{
		ID:            u.ID,
		Username:      u.Username,
		RealName:      u.RealName,
		SecurityLevel: u.SecurityLevel,
		CreatedAt:     u.CreatedAt.Format(time.RFC3339),
		TotalCalls:    u.TotalCalls,
	}
	if u.LastLoginAt.Valid {
		formatted := u.LastLoginAt.Time.Format(time.RFC3339)
		dto.LastLoginAt = &formatted
	}
	return dto
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Users.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	dtos := make([]userDTO, len(users))
	for i, u := range users {
		dtos[i] = toUserDTO(u)
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleSetUserSecurityLevel updates a user's security level and/or
// real name -- one combined PUT since the admin Users page edits both
// on the same row with a single Save button. RealName is a plain
// *string (as opposed to SecurityLevel, which is always sent) so a
// request that only means to change the level can omit it entirely
// without accidentally clearing an existing real name.
func (s *Server) handleSetUserSecurityLevel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var body struct {
		SecurityLevel int     `json:"security_level"`
		RealName      *string `json:"real_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.SecurityLevel < 0 || body.SecurityLevel > 255 {
		writeError(w, http.StatusBadRequest, "security_level must be between 0 and 255")
		return
	}

	if err := s.Users.SetSecurityLevel(id, body.SecurityLevel); err != nil {
		switch {
		case errors.Is(err, user.ErrLastSysop):
			writeError(w, http.StatusConflict, "cannot demote the last sysop-level account")
		case errors.Is(err, user.ErrNotFound):
			writeError(w, http.StatusNotFound, "user not found")
		default:
			writeError(w, http.StatusInternalServerError, "could not update security level")
		}
		return
	}
	if body.RealName != nil {
		if err := s.Users.SetRealName(id, *body.RealName); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update real name")
			return
		}
	}

	updated, err := s.Users.ByID(id)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load updated user")
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s set %s's security level to %d (via web)", claims.Subject, updated.Username, updated.SecurityLevel)
	}

	writeJSON(w, http.StatusOK, toUserDTO(*updated))
}
