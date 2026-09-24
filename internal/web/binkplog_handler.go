package web

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/binkplog"
)

type binkpSessionDTO struct {
	ID          int64  `json:"id"`
	Direction   string `json:"direction"`
	PeerAddress string `json:"peer_address"`
	PeerHost    string `json:"peer_host"`
	StartedAt   string `json:"started_at"`
	SizeBytes   int64  `json:"size_bytes"`
	Outcome     string `json:"outcome"`
	Detail      string `json:"detail"`
}

func toBinkpSessionDTO(e binkplog.Entry) binkpSessionDTO {
	return binkpSessionDTO{
		ID:          e.ID,
		Direction:   e.Direction,
		PeerAddress: e.PeerAddress,
		PeerHost:    e.PeerHost,
		StartedAt:   e.StartedAt.Format(time.RFC3339),
		SizeBytes:   e.SizeBytes,
		Outcome:     e.Outcome,
		Detail:      e.Detail,
	}
}

// handleListBinkpSessions lists recorded BinkP session transcripts
// (see internal/binkplog), most recently started first -- the admin
// Logs page's BinkP tab.
func (s *Server) handleListBinkpSessions(w http.ResponseWriter, r *http.Request) {
	if s.BinkpLog == nil {
		writeError(w, http.StatusInternalServerError, "binkp session log is not configured")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.BinkpLog.Recent(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list binkp sessions")
		return
	}
	dtos := make([]binkpSessionDTO, len(entries))
	for i, e := range entries {
		dtos[i] = toBinkpSessionDTO(e)
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleGetBinkpSessionTranscript streams id's full recorded
// transcript as plain text.
func (s *Server) handleGetBinkpSessionTranscript(w http.ResponseWriter, r *http.Request) {
	if s.BinkpLog == nil {
		writeError(w, http.StatusInternalServerError, "binkp session log is not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	rc, err := s.BinkpLog.Open(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "binkp session not found")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.Copy(w, rc); err != nil {
		s.logWarn("streaming binkp session %d transcript: %v", id, err)
	}
}
