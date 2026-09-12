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
	SecurityLevel int     `json:"security_level"`
	CreatedAt     string  `json:"created_at"`
	LastLoginAt   *string `json:"last_login_at"`
	TotalCalls    int     `json:"total_calls"`
}

func toUserDTO(u user.User) userDTO {
	dto := userDTO{
		ID:            u.ID,
		Username:      u.Username,
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

func (s *Server) handleSetUserSecurityLevel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var body struct {
		SecurityLevel int `json:"security_level"`
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

	updated, err := s.Users.ByID(id)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load updated user")
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(*updated))
}
