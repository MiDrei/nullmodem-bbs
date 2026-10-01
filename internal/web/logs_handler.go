package web

import (
	"errors"
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

// handleListLogs serves the log viewer: GET /api/logs returns the
// newest entries of a category (see applog.Filter), oldest first.
// Parameters: category (mailer, system, telnet, ssh, web; none for
// all), level (repeatable, info/warn/error), prefix and
// exclude_prefix (repeatable; a door's lines start with "Name:"), q
// (text), before_id (older ones, paging back), after_id (newer ones,
// polling), limit (default 200, at most 1000).
func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := applog.Filter{
		Category:        q.Get("category"),
		Prefixes:        q["prefix"],
		ExcludePrefixes: q["exclude_prefix"],
		Query:           q.Get("q"),
		Limit:           defaultLogLimit,
	}
	for _, l := range q["level"] {
		f.Levels = append(f.Levels, applog.Level(l))
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		f.Limit = n
	}
	for name, dst := range map[string]*int64{"after_id": &f.AfterID, "before_id": &f.BeforeID} {
		if v := q.Get(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid "+name)
				return
			}
			*dst = n
		}
	}
	entries, err := s.Logs.List(f)
	if errors.Is(err, applog.ErrUnknownCategory) {
		writeError(w, http.StatusBadRequest, "unknown category")
		return
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
