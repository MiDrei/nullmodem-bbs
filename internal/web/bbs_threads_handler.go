package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/midrei/nullmodem-kit/ansi"
)

// Threads for the portal and the reader app: a message's whole thread
// (GET /api/bbs/messages/{id}/thread) and an area's threads by latest
// activity (GET /api/bbs/message-areas/{id}/threads).

type bbsThreadEntryDTO struct {
	ID       int64  `json:"id"`
	ReplyTo  int64  `json:"reply_to,omitempty"`
	Depth    int    `json:"depth"`
	FromName string `json:"from_name"`
	ToName   string `json:"to_name"`
	Subject  string `json:"subject"`
	PostedAt string `json:"posted_at"`
	Read     bool   `json:"read"`
	// Guessed: linked by its "Re:" subject only.
	Guessed bool `json:"guessed,omitempty"`
}

type bbsThreadSummaryDTO struct {
	ID       int64  `json:"id"`
	FromName string `json:"from_name"`
	Subject  string `json:"subject"`
	PostedAt string `json:"posted_at"`
	Replies  int    `json:"replies"`
	Unread   int    `json:"unread"`
	LastAt   string `json:"last_at"`
	LastFrom string `json:"last_from"`
}

func (s *Server) handleBBSThread(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	m, err := s.Messages.MessageByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	area, err := s.Messages.AreaByID(m.AreaID)
	if err != nil || !area.CanRead(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to read this message")
		return
	}
	entries, err := s.Messages.ThreadOf(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the thread")
		return
	}
	read, err := s.Messages.ReadMessageIDs(claims.UserID, m.AreaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the thread")
		return
	}
	out := make([]bbsThreadEntryDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, bbsThreadEntryDTO{
			ID: e.ID, ReplyTo: e.ReplyTo, Depth: e.Depth,
			FromName: ansi.DecodeCP437([]byte(e.FromName)), ToName: ansi.DecodeCP437([]byte(e.ToName)),
			Subject: ansi.DecodeCP437([]byte(e.Subject)), PostedAt: e.PostedAt.Format(time.RFC3339),
			Read: read[e.ID], Guessed: e.Guessed,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"area_id": m.AreaID, "messages": out})
}

func (s *Server) handleBBSAreaThreads(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	areaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	area, err := s.Messages.AreaByID(areaID)
	if err != nil {
		writeError(w, http.StatusNotFound, "message area not found")
		return
	}
	if !area.CanRead(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to read this message area")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	threads, total, err := s.Messages.AreaThreads(areaID, claims.UserID, limit, max(0, offset))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the threads")
		return
	}
	out := make([]bbsThreadSummaryDTO, 0, len(threads))
	for _, t := range threads {
		out = append(out, bbsThreadSummaryDTO{
			ID: t.Root.ID, FromName: ansi.DecodeCP437([]byte(t.Root.FromName)), Subject: ansi.DecodeCP437([]byte(t.Root.Subject)),
			PostedAt: t.Root.PostedAt.Format(time.RFC3339), Replies: t.Replies, Unread: t.Unread,
			LastAt: t.LastAt.Format(time.RFC3339), LastFrom: ansi.DecodeCP437([]byte(t.LastFrom)),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": out, "total": total})
}
