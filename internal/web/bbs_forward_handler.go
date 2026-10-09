package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/emailgw"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// forwardDTO is the caller's netmail forwarding to an address of
// theirs (see internal/emailgw's forward.go).
type forwardDTO struct {
	Address  string `json:"address"`
	Verified bool   `json:"verified"`
	Pending  bool   `json:"pending"`
	MarkRead bool   `json:"mark_read"`
}

// forwardCaller is the logged-in caller, the gateway's settings and
// whether they may forward at all (writes the error if not).
func (s *Server) forwardCaller(w http.ResponseWriter, r *http.Request) (*user.User, config.EmailConfig, bool) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return nil, config.EmailConfig{}, false
	}
	u, err := s.Users.ByID(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return nil, config.EmailConfig{}, false
	}
	c, err := s.loadBBSConfig()
	if err != nil || s.EmailGateway == nil || !emailgw.May(c.Email, u) {
		writeError(w, http.StatusBadRequest, "email forwarding isn't available for you")
		return nil, config.EmailConfig{}, false
	}
	return u, c.Email, true
}

// handleRequestForward: POST /api/bbs/profile/forward {address} --
// mails a confirmation code there.
func (s *Server) handleRequestForward(w http.ResponseWriter, r *http.Request) {
	u, cfg, ok := s.forwardCaller(w, r)
	if !ok {
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := emailgw.NewForwards(s.DB).Request(cfg, u, req.Address, s.EmailGateway.Lang(u), s.EmailGateway.BBSName(), time.Now())
	switch {
	case errors.Is(err, emailgw.ErrForwardAddress):
		writeError(w, http.StatusBadRequest, "that's not an email address")
		return
	case errors.Is(err, emailgw.ErrForwardOwnDomain):
		writeError(w, http.StatusBadRequest, "an address of this board's own domains can't be used")
		return
	case errors.Is(err, emailgw.ErrCodeTooSoon):
		writeError(w, http.StatusTooManyRequests, "a code was sent less than a minute ago")
		return
	case err != nil:
		s.logWarn("%s: forwarding code not sent: %v", u.Username, err)
		writeError(w, http.StatusBadGateway, "the code could not be sent, try again later")
		return
	}
	s.logInfo("%s asked for netmail forwarding to an address of theirs via the BBS portal", u.Username)
	writeJSON(w, http.StatusOK, s.profileFor(u))
}

// handleConfirmForward: POST /api/bbs/profile/forward/confirm {code}.
func (s *Server) handleConfirmForward(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.forwardCaller(w, r)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch err := emailgw.NewForwards(s.DB).Confirm(u.ID, req.Code, time.Now()); {
	case errors.Is(err, emailgw.ErrNoCode):
		writeError(w, http.StatusBadRequest, "no code was asked for")
		return
	case errors.Is(err, emailgw.ErrCodeExpired):
		writeError(w, http.StatusBadRequest, "the code expired; ask for a new one")
		return
	case errors.Is(err, emailgw.ErrCodeWrong):
		writeError(w, http.StatusBadRequest, "wrong code")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not save the forwarding")
		return
	}
	s.logInfo("%s turned on netmail forwarding via the BBS portal", u.Username)
	writeJSON(w, http.StatusOK, s.profileFor(u))
}

// handleUpdateForward: PUT /api/bbs/profile/forward {mark_read}.
func (s *Server) handleUpdateForward(w http.ResponseWriter, r *http.Request) {
	u, _, ok := s.forwardCaller(w, r)
	if !ok {
		return
	}
	var req struct {
		MarkRead bool `json:"mark_read"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := emailgw.NewForwards(s.DB).SetMarkRead(u.ID, req.MarkRead); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the forwarding")
		return
	}
	writeJSON(w, http.StatusOK, s.profileFor(u))
}

// handleRemoveForward: DELETE /api/bbs/profile/forward.
func (s *Server) handleRemoveForward(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	if err := emailgw.NewForwards(s.DB).Remove(claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the forwarding")
		return
	}
	s.logInfo("%s turned off netmail forwarding via the BBS portal", claims.Subject)
	u, err := s.Users.ByID(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}
	writeJSON(w, http.StatusOK, s.profileFor(u))
}
