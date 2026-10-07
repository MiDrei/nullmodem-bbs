package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/guard"
)

// The web admin's Security page: login protection (internal/guard) and
// new-user approval -- settings, the addresses locked out now, the
// allow and block lists, the latest failed logins.

type securitySettingsDTO struct {
	LockoutEnabled      bool     `json:"lockout_enabled"`
	MaxFailures         int      `json:"max_failures"`
	WindowMinutes       int      `json:"window_minutes"`
	LockoutMinutes      int      `json:"lockout_minutes"`
	MaxLockoutHours     int      `json:"max_lockout_hours"`
	MaxConnectionsPerIP int      `json:"max_connections_per_ip"`
	IdleMinutes         int      `json:"idle_minutes"`
	ApproveNewUsers     bool     `json:"approve_new_users"`
	PendingSL           int      `json:"pending_sl"`
	NewUserSL           int      `json:"new_user_sl"`
	BlockedHandles      []string `json:"blocked_handles"`
	// RequireAdminTOTP: no admin (or Telnet sysop menu) without
	// two-factor login.
	RequireAdminTOTP bool `json:"require_admin_totp"`
}

type securityResponse struct {
	Settings securitySettingsDTO `json:"settings"`
	Lockouts []guard.Lockout     `json:"lockouts"`
	Rules    []guard.Rule        `json:"rules"`
	Failures []guard.Failure     `json:"failures"`
	// YourIP is the address this request came from -- to put on the
	// allow list.
	YourIP string `json:"your_ip"`
}

func toSecurityDTO(c *config.Config) securitySettingsDTO {
	sc := c.Security
	blocked := sc.BlockedHandles
	if blocked == nil {
		blocked = []string{}
	}
	return securitySettingsDTO{
		LockoutEnabled: sc.Lockout(), MaxFailures: sc.Failures(), WindowMinutes: sc.Window(),
		LockoutMinutes: sc.LockoutMins(), MaxLockoutHours: sc.MaxLockout(), MaxConnectionsPerIP: sc.MaxConnections(),
		IdleMinutes: sc.Idle(), ApproveNewUsers: sc.Approval(), PendingSL: sc.Pending(), NewUserSL: c.BBS.NewUserSL, BlockedHandles: blocked,
		RequireAdminTOTP: sc.RequireAdminTOTP,
	}
}

func (s *Server) securityResponse(r *http.Request, c *config.Config) (securityResponse, error) {
	resp := securityResponse{Settings: toSecurityDTO(c), YourIP: clientIP(r),
		Lockouts: []guard.Lockout{}, Rules: []guard.Rule{}, Failures: []guard.Failure{}}
	if s.Guard == nil {
		return resp, nil
	}
	var err error
	if resp.Lockouts, err = s.Guard.Lockouts(); err != nil {
		return resp, err
	}
	if resp.Rules, err = s.Guard.Rules(); err != nil {
		return resp, err
	}
	resp.Failures, err = s.Guard.RecentFailures(50)
	return resp, err
}

func (s *Server) writeSecurity(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	resp, err := s.securityResponse(r, c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the login protection state")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetSecurity(w http.ResponseWriter, r *http.Request) {
	s.writeSecurity(w, r)
}

// handlePutSecuritySettings saves the settings; the daemons re-read
// them within half a minute.
func (s *Server) handlePutSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var d securitySettingsDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch {
	case d.MaxFailures < 1 || d.WindowMinutes < 1 || d.LockoutMinutes < 1 || d.MaxLockoutHours < 1:
		writeError(w, http.StatusBadRequest, "the lockout limits must be at least 1")
		return
	case d.IdleMinutes < 0:
		writeError(w, http.StatusBadRequest, "idle minutes must be 0 or more")
		return
	case d.MaxConnectionsPerIP < 0:
		writeError(w, http.StatusBadRequest, "connections per address must not be negative (0: no limit)")
		return
	case d.PendingSL < 0 || d.PendingSL > 254 || d.NewUserSL < 1 || d.NewUserSL > 254:
		writeError(w, http.StatusBadRequest, "security levels must be 0-254")
		return
	case d.ApproveNewUsers && d.PendingSL >= d.NewUserSL:
		writeError(w, http.StatusBadRequest, "the waiting level must be below the new-user level")
		return
	}
	if d.RequireAdminTOTP {
		// Only with two-factor on oneself -- else it locks the asker out.
		if claims, ok := claimsFromContext(r.Context()); ok {
			if me, err := s.Users.ByID(claims.UserID); err != nil || !me.TwoFactor {
				writeError(w, http.StatusBadRequest, "turn on two-factor login for your own account first")
				return
			}
		}
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	var handles []string
	for _, h := range d.BlockedHandles {
		if h = strings.TrimSpace(h); h != "" {
			handles = append(handles, h)
		}
	}
	p := func(v int) *int { return &v }
	b := func(v bool) *bool { return &v }
	c.Security = config.SecurityConfig{
		LockoutEnabled: b(d.LockoutEnabled), MaxFailures: p(d.MaxFailures), WindowMinutes: p(d.WindowMinutes),
		LockoutMinutes: p(d.LockoutMinutes), MaxLockoutHours: p(d.MaxLockoutHours), MaxConnectionsPerIP: p(d.MaxConnectionsPerIP),
		IdleMinutes: p(d.IdleMinutes), ApproveNewUsers: b(d.ApproveNewUsers), PendingSL: p(d.PendingSL), BlockedHandles: handles,
		RequireAdminTOTP: d.RequireAdminTOTP,
	}
	c.BBS.NewUserSL = d.NewUserSL
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s changed the security settings", claims.Subject)
	}
	s.writeSecurity(w, r)
}

type ipRequest struct {
	IP      string `json:"ip"`
	Pattern string `json:"pattern"`
	Kind    string `json:"kind"`
	Note    string `json:"note"`
}

func (s *Server) guardReady(w http.ResponseWriter) bool {
	if s.Guard == nil {
		writeError(w, http.StatusServiceUnavailable, "login protection is not available")
		return false
	}
	return true
}

// handleUnlockIP: POST /api/security/unlock {ip}.
func (s *Server) handleUnlockIP(w http.ResponseWriter, r *http.Request) {
	var req ipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		writeError(w, http.StatusBadRequest, "ip is required")
		return
	}
	if !s.guardReady(w) {
		return
	}
	if err := s.Guard.Unlock(req.IP); err != nil {
		writeError(w, http.StatusInternalServerError, "could not unlock")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s unlocked %s", claims.Subject, req.IP)
	}
	s.writeSecurity(w, r)
}

// handleAddIPRule: POST /api/security/rules {pattern, kind, note}.
func (s *Server) handleAddIPRule(w http.ResponseWriter, r *http.Request) {
	var req ipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !s.guardReady(w) {
		return
	}
	pattern, err := s.Guard.AddRule(req.Pattern, req.Kind, req.Note)
	if errors.Is(err, guard.ErrBadPattern) {
		writeError(w, http.StatusBadRequest, "not an IP address or CIDR range (e.g. 203.0.113.5 or 203.0.113.0/24)")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s put %s on the %s list", claims.Subject, pattern, req.Kind)
	}
	s.writeSecurity(w, r)
}

// handleDeleteIPRule: DELETE /api/security/rules {pattern}.
func (s *Server) handleDeleteIPRule(w http.ResponseWriter, r *http.Request) {
	var req ipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
		writeError(w, http.StatusBadRequest, "pattern is required")
		return
	}
	if !s.guardReady(w) {
		return
	}
	if err := s.Guard.DeleteRule(req.Pattern); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete the rule")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s took %s off the allow/block list", claims.Subject, req.Pattern)
	}
	s.writeSecurity(w, r)
}
