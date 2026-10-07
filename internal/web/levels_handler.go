package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/i18n"
)

// levelsDTO is the named security levels; Custom is false while the
// board shows its own defaults (none set by the sysop).
type levelsDTO struct {
	Levels []config.SecurityLevel `json:"levels"`
	Custom bool                   `json:"custom"`
}

func (s *Server) levelsResponse(r *http.Request, c *config.Config) levelsDTO {
	lang := s.requestLang(r)
	return levelsDTO{Levels: c.Levels(func(k string) string { return i18n.T(lang, k) }), Custom: len(c.SecurityLevels) > 0}
}

// handleGetSecurityLevels: GET /api/security-levels (admin).
func (s *Server) handleGetSecurityLevels(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.levelsResponse(r, c))
}

// handlePutSecurityLevels: PUT /api/security-levels {levels} (admin)
// -- an empty list goes back to the board's own levels.
func (s *Server) handlePutSecurityLevels(w http.ResponseWriter, r *http.Request) {
	var req levelsDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	seen := map[int]bool{}
	var levels []config.SecurityLevel
	for _, l := range req.Levels {
		l.Name = strings.TrimSpace(l.Name)
		if l.Level < 0 || l.Level > 255 || l.Name == "" || utf8.RuneCountInString(l.Name) > 40 {
			writeError(w, http.StatusBadRequest, "a level needs a number from 0 to 255 and a name of up to 40 characters")
			return
		}
		if seen[l.Level] {
			writeError(w, http.StatusBadRequest, "each level may be named only once")
			return
		}
		seen[l.Level] = true
		levels = append(levels, l)
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	c.SecurityLevels = levels
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s changed the security levels' names", claims.Subject)
	}
	writeJSON(w, http.StatusOK, s.levelsResponse(r, c))
}
