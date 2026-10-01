package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

type userDTO struct {
	ID            int64   `json:"id"`
	Username      string  `json:"username"`
	RealName      string  `json:"real_name"`
	SecurityLevel int     `json:"security_level"`
	CreatedAt     string  `json:"created_at"`
	LastLoginAt   *string `json:"last_login_at"`
	TotalCalls    int     `json:"total_calls"`
	// Validated is false while the account waits for approval.
	Validated bool `json:"validated"`
}

func toUserDTO(u user.User) userDTO {
	dto := userDTO{
		ID:            u.ID,
		Username:      u.Username,
		RealName:      u.RealName,
		SecurityLevel: u.SecurityLevel,
		CreatedAt:     u.CreatedAt.Format(time.RFC3339),
		TotalCalls:    u.TotalCalls,
		Validated:     u.Validated,
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

// handleApproveUser: POST /api/users/{id}/approve -- an account waiting
// for approval gets the new-user level (bbs.new_user_sl) and may post.
func (s *Server) handleApproveUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if err := s.Users.Approve(id, c.BBS.NewUserSL); errors.Is(err, user.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such user")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not approve the user")
		return
	}
	u, err := s.Users.ByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the user")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s approved the new account %s (SL %d)", claims.Subject, u.Username, u.SecurityLevel)
	}
	writeJSON(w, http.StatusOK, toUserDTO(*u))
}

// handleDeletePendingUser: DELETE /api/users/{id} -- only an account
// still waiting for approval (one turned down).
func (s *Server) handleDeletePendingUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	u, err := s.Users.ByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such user")
		return
	}
	switch err := s.Users.DeletePending(id); {
	case errors.Is(err, user.ErrNotPending):
		writeError(w, http.StatusConflict, "only an account waiting for approval can be deleted")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not delete the user")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s turned down and deleted the new account %s", claims.Subject, u.Username)
	}
	w.WriteHeader(http.StatusNoContent)
}
