package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
)

// errNotUnresolved is returned by unresolvedByID for a message that
// exists but isn't genuinely unresolved (see its own doc comment) --
// callers only ever check err != nil (both cases become a 404, same
// as a nonexistent ID), so this stays unexported.
var errNotUnresolved = errors.New("web: message is not unresolved")

// unresolvedNetmailLimit bounds how many of the most recent
// unresolved-recipient messages (see netmail.Store.UnresolvedInbox)
// the admin panel lists -- mirrors internal/bbs's own
// unresolvedNetmailLimit (its Telnet/SSH sysop netmail view merges
// the same thing in): generous, since this is meant to surface
// undeliverable mail (a mistyped recipient, an Areafix/Filefix
// robot's reply) promptly, not accumulate an unbounded backlog.
const unresolvedNetmailLimit = 50

type unresolvedNetmailSummaryDTO struct {
	ID          int64  `json:"id"`
	FromName    string `json:"from_name"`
	FromAddress string `json:"from_address"`
	ToName      string `json:"to_name"`
	Subject     string `json:"subject"`
	PostedAt    string `json:"posted_at"`
}

// toUnresolvedNetmailSummaryDTO decodes FromName/ToName/Subject from
// raw CP437 bytes to safe UTF-8 -- see bbs_messages_handler.go's
// toBBSMessageDTO doc comment for why this bridge exists.
func toUnresolvedNetmailSummaryDTO(m netmail.Message) unresolvedNetmailSummaryDTO {
	return unresolvedNetmailSummaryDTO{
		ID:          m.ID,
		FromName:    ansi.DecodeCP437([]byte(m.FromName)),
		FromAddress: m.FromAddress,
		ToName:      ansi.DecodeCP437([]byte(m.ToName)),
		Subject:     ansi.DecodeCP437([]byte(m.Subject)),
		PostedAt:    m.PostedAt.Format(time.RFC3339),
	}
}

// handleListUnresolvedNetmail returns inbound netmail whose recipient
// name never resolved to a real local user, and that has no remote
// FTN destination either (see netmail.Store.UnresolvedInbox) -- a
// mistyped username, or a reply from an automated robot (Areafix/
// Filefix, ...) addressed back to whatever name this system used as
// its own request's From. Stored (never silently discarded) but
// otherwise invisible anywhere in the BBS: internal/bbs's own Telnet/
// SSH sysop netmail view already merges this in (see showNetmail);
// this is that same visibility for the web admin panel.
func (s *Server) handleListUnresolvedNetmail(w http.ResponseWriter, r *http.Request) {
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "netmail is not configured")
		return
	}
	msgs, err := s.Netmail.UnresolvedInbox(unresolvedNetmailLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list unresolved netmail")
		return
	}
	dtos := make([]unresolvedNetmailSummaryDTO, len(msgs))
	for i, m := range msgs {
		dtos[i] = toUnresolvedNetmailSummaryDTO(m)
	}
	writeJSON(w, http.StatusOK, dtos)
}

type unresolvedNetmailDTO struct {
	ID          int64  `json:"id"`
	FromName    string `json:"from_name"`
	FromAddress string `json:"from_address"`
	ToName      string `json:"to_name"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	BodyHTML    string `json:"body_html"`
	PostedAt    string `json:"posted_at"`
}

// unresolvedByID loads id and confirms it's genuinely an unresolved
// message (no local recipient, no remote FTN destination either --
// the same WHERE UnresolvedInbox itself queries by) rather than just
// trusting a caller-supplied ID, so this endpoint can't be used to
// read or delete someone's ordinary netmail by guessing its number.
func (s *Server) unresolvedByID(id int64) (*netmail.Message, error) {
	m, err := s.Netmail.MessageByID(id)
	if err != nil {
		return nil, err
	}
	if m.ToUserID.Valid || m.ToAddress != "" {
		return nil, errNotUnresolved
	}
	return m, nil
}

// handleGetUnresolvedNetmail loads one unresolved message's full body.
func (s *Server) handleGetUnresolvedNetmail(w http.ResponseWriter, r *http.Request) {
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "netmail is not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	m, err := s.unresolvedByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "unresolved netmail message not found")
		return
	}
	writeJSON(w, http.StatusOK, unresolvedNetmailDTO{
		ID:          m.ID,
		FromName:    ansi.DecodeCP437([]byte(m.FromName)),
		FromAddress: m.FromAddress,
		ToName:      ansi.DecodeCP437([]byte(m.ToName)),
		Subject:     ansi.DecodeCP437([]byte(m.Subject)),
		Body:        ansi.DecodeCP437([]byte(m.Body)),
		BodyHTML:    ansi.ToHTML(m.Body),
		PostedAt:    m.PostedAt.Format(time.RFC3339),
	})
}

// handleDeleteUnresolvedNetmail permanently dismisses one unresolved
// message -- there's nowhere else for a sysop to route or reply to
// it, so deleting it (rather than marking it read) is the only
// meaningful way to clear it off this list.
func (s *Server) handleDeleteUnresolvedNetmail(w http.ResponseWriter, r *http.Request) {
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "netmail is not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	if _, err := s.unresolvedByID(id); err != nil {
		writeError(w, http.StatusNotFound, "unresolved netmail message not found")
		return
	}
	if err := s.Netmail.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete message")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted unresolved netmail %d", claims.Subject, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBatchDeleteUnresolvedNetmail dismisses several unresolved
// messages at once (the admin UI's multi-select "Delete selected") --
// same semantics as handleDeleteUnresolvedNetmail per ID: a
// nonexistent or already-resolved ID is skipped rather than failing
// the whole batch, since a concurrent sysop or a stale UI selection
// shouldn't stop the rest from being cleared.
func (s *Server) handleBatchDeleteUnresolvedNetmail(w http.ResponseWriter, r *http.Request) {
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "netmail is not configured")
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids must not be empty")
		return
	}

	var deleted int
	for _, id := range req.IDs {
		if _, err := s.unresolvedByID(id); err != nil {
			continue
		}
		if err := s.Netmail.Delete(id); err != nil {
			writeError(w, http.StatusInternalServerError, "could not delete message")
			return
		}
		deleted++
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted %d unresolved netmail message(s)", claims.Subject, deleted)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}
