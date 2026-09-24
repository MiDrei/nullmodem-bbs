package web

import (
	"net/http"
	"strconv"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/applog"
)

const defaultLogLimit = 200

type logEntryDTO struct {
	ID       int64  `json:"id"`
	LoggedAt string `json:"logged_at"`
	Source   string `json:"source"`
	Level    string `json:"level"`
	Message  string `json:"message"`
}

func toLogEntryDTO(e applog.Entry) logEntryDTO {
	return logEntryDTO{
		ID:       e.ID,
		LoggedAt: e.LoggedAt.Format(time.RFC3339),
		Source:   e.Source,
		Level:    string(e.Level),
		Message:  e.Message,
	}
}

// handleListLogs serves the log viewer: GET /api/logs?limit=N returns
// the N most recent entries (for the initial page load), and
// GET /api/logs?after_id=X&limit=N returns entries logged after id X
// (for polling new entries without re-fetching the whole list).
func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	limit := defaultLogLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = n
	}

	var (
		entries []applog.Entry
		err     error
	)
	if v := r.URL.Query().Get("after_id"); v != "" {
		afterID, convErr := strconv.ParseInt(v, 10, 64)
		if convErr != nil {
			writeError(w, http.StatusBadRequest, "invalid after_id")
			return
		}
		entries, err = s.Logs.Since(afterID, limit)
	} else {
		entries, err = s.Logs.Recent(limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list logs")
		return
	}

	dtos := make([]logEntryDTO, len(entries))
	for i, e := range entries {
		dtos[i] = toLogEntryDTO(e)
	}
	writeJSON(w, http.StatusOK, dtos)
}
