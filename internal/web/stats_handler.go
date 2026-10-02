package web

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/stats"
)

var (
	publicStatsMu     sync.Mutex
	publicStatsCached *stats.Report
	publicStatsAt     time.Time
)

// handlePublicStats: GET /api/public/stats (no login), the last 30
// days, cached for five minutes.
func (s *Server) handlePublicStats(w http.ResponseWriter, r *http.Request) {
	if s.Stats == nil {
		writeError(w, http.StatusNotFound, "no statistics")
		return
	}
	publicStatsMu.Lock()
	defer publicStatsMu.Unlock()
	if publicStatsCached == nil || time.Since(publicStatsAt) > 5*time.Minute {
		rep, err := s.Stats.Report(30, false)
		if err != nil {
			s.logWarn("%v", err)
			writeError(w, http.StatusInternalServerError, "could not load the statistics")
			return
		}
		publicStatsCached, publicStatsAt = rep, time.Now()
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, publicStatsCached)
}

// handleStats: GET /api/stats?days=30 (admin), everything.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.Stats == nil {
		writeError(w, http.StatusNotFound, "no statistics")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 366 {
		days = 30
	}
	rep, err := s.Stats.Report(days, true)
	if err != nil {
		s.logWarn("%v", err)
		writeError(w, http.StatusInternalServerError, "could not load the statistics")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
