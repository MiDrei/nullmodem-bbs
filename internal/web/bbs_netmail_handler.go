package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/kit/ansi"
)

type bbsNetmailSummaryDTO struct {
	ID        int64  `json:"id"`
	FromName  string `json:"from_name"`
	ToName    string `json:"to_name"`
	Subject   string `json:"subject"`
	PostedAt  string `json:"posted_at"`
	Unread    bool   `json:"unread"`
	FromLocal bool   `json:"from_local"`
}

// toBBSNetmailSummaryDTO decodes FromName/ToName/Subject from raw
// CP437 bytes to safe UTF-8 -- see bbs_messages_handler.go's
// toBBSMessageDTO doc comment for why this bridge exists.
func toBBSNetmailSummaryDTO(m netmail.Message) bbsNetmailSummaryDTO {
	return bbsNetmailSummaryDTO{
		ID:        m.ID,
		FromName:  ansi.DecodeCP437([]byte(m.FromName)),
		ToName:    ansi.DecodeCP437([]byte(m.ToName)),
		Subject:   ansi.DecodeCP437([]byte(m.Subject)),
		PostedAt:  m.PostedAt.Format(time.RFC3339),
		Unread:    !m.IsRead(),
		FromLocal: !m.IsFromRemote(),
	}
}

// handleListBBSNetmail returns the caller's own netmail inbox --
// unlike internal/bbs's own sysop-facing view, this never merges in
// UnresolvedInbox (mail whose recipient name didn't resolve to any
// local account can't belong to a specific logged-in caller by
// definition).
func (s *Server) handleListBBSNetmail(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	msgs, err := s.Netmail.Inbox(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list netmail")
		return
	}
	dtos := make([]bbsNetmailSummaryDTO, len(msgs))
	for i, m := range msgs {
		dtos[i] = toBBSNetmailSummaryDTO(m)
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleListBBSNetmailSent returns the caller's own sent netmail --
// Sent's counterpart to handleListBBSNetmail's Inbox.
func (s *Server) handleListBBSNetmailSent(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	msgs, err := s.Netmail.Sent(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list sent netmail")
		return
	}
	dtos := make([]bbsNetmailSummaryDTO, len(msgs))
	for i, m := range msgs {
		dtos[i] = toBBSNetmailSummaryDTO(m)
	}
	writeJSON(w, http.StatusOK, dtos)
}

type bbsNetmailDTO struct {
	ID          int64  `json:"id"`
	FromName    string `json:"from_name"`
	FromAddress string `json:"from_address"`
	ToName      string `json:"to_name"`
	ToAddress   string `json:"to_address"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	BodyHTML    string `json:"body_html"`
	// Preformatted/Grid mirror bbsMessageDTO's own fields -- see their
	// doc comment.
	Preformatted bool       `json:"preformatted"`
	Grid         *ansi.Grid `json:"grid,omitempty"`
	PostedAt     string     `json:"posted_at"`
	// PrevID/NextID mirror bbsMessageDTO's own fields (see its doc
	// comment) -- within the caller's own inbox instead of an area.
	PrevID *int64 `json:"prev_id,omitempty"`
	NextID *int64 `json:"next_id,omitempty"`
	// IsRecipient is false when the caller is viewing this from their
	// own Sent list (they were the sender, not the recipient) -- the
	// frontend uses it to hide Reply/Delete, which only make sense for
	// the recipient.
	IsRecipient bool `json:"is_recipient"`
}

// toBBSNetmailDTO is toBBSMessageDTO's netmail counterpart -- see its
// doc comment for the CP437-bytes-on-disk / UTF-8-over-JSON bridge
// this performs. FromAddress/ToAddress are FTN routing addresses
// (zone:net/node.point), always plain ASCII, so they need no decoding.
func toBBSNetmailDTO(m netmail.Message) bbsNetmailDTO {
	preformatted := ansi.IsPreformatted(m.Body)
	var grid *ansi.Grid
	if preformatted {
		g := ansi.ParseGrid(m.Body, artWidth)
		grid = &g
	}
	return bbsNetmailDTO{
		ID:           m.ID,
		FromName:     ansi.DecodeCP437([]byte(m.FromName)),
		FromAddress:  m.FromAddress,
		ToName:       ansi.DecodeCP437([]byte(m.ToName)),
		ToAddress:    m.ToAddress,
		Subject:      ansi.DecodeCP437([]byte(m.Subject)),
		Body:         ansi.DecodeCP437([]byte(m.Body)),
		BodyHTML:     ansi.ToHTML(m.Body),
		Preformatted: preformatted,
		Grid:         grid,
		PostedAt:     m.PostedAt.Format(time.RFC3339),
	}
}

// handleGetBBSNetmail loads one netmail message -- the recipient may
// open it (which also marks it read), and so may the original sender
// looking at their own Sent list (an FTN-addressed message has no
// local recipient to ever be "read" by at all, and even a
// local-recipient message shouldn't be marked read just because its
// own sender re-opened it). Anyone else has no legitimate reason to
// read someone else's private mail just because they know its
// numeric ID, mirroring internal/bbs's own netmail reader.
func (s *Server) handleGetBBSNetmail(w http.ResponseWriter, r *http.Request) {
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
	m, err := s.Netmail.MessageByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "netmail message not found")
		return
	}
	isRecipient := m.ToUserID.Valid && m.ToUserID.Int64 == claims.UserID
	isSender := m.FromUserID.Valid && m.FromUserID.Int64 == claims.UserID
	if !isRecipient && !isSender {
		writeError(w, http.StatusForbidden, "not permitted to read this message")
		return
	}

	dto := toBBSNetmailDTO(*m)
	dto.IsRecipient = isRecipient
	if isRecipient {
		if err := s.Netmail.MarkRead(id); err != nil {
			writeError(w, http.StatusInternalServerError, "could not mark message read")
			return
		}
		if prev, next, err := s.Netmail.Neighbors(claims.UserID, m.ID); err == nil {
			dto.PrevID, dto.NextID = prev, next
		}
	} else if prev, next, err := s.Netmail.SentNeighbors(claims.UserID, m.ID); err == nil {
		dto.PrevID, dto.NextID = prev, next
	}
	writeJSON(w, http.StatusOK, dto)
}

// handleSendBBSNetmail composes a new netmail message as the logged-in
// caller. Recipient resolution mirrors internal/bbs/netmail.go's own
// compose flow exactly: a name that resolves to a local account is
// delivered immediately (ToUserID set); anything shaped like an FTN
// address (see netmail.IsFTNAddress) is queued for internal/tosser to
// route instead. Anything else is rejected outright, same as the
// Telnet/SSH session would refuse to send it.
func (s *Server) handleSendBBSNetmail(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	var req struct {
		To      string `json:"to"`
		ToName  string `json:"to_name"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Crash   bool   `json:"crash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.To == "" || req.Subject == "" || req.Body == "" {
		writeError(w, http.StatusBadRequest, "to, subject and body are required")
		return
	}

	// ToName only matters for an FTN address: a node has many possible
	// recipients, so the address alone doesn't say who the remote
	// sysop should hand the message to (mirrors internal/bbs's
	// composeNetmail "Recipient name" prompt, including its fallback
	// to the address itself when left blank). A local username is
	// unambiguous on its own, so any ToName is ignored for it.
	var toUserID int64
	var toName, toAddress string
	if recipient, err := s.Users.ByUsername(req.To); err == nil {
		toUserID = recipient.ID
		toName = recipient.Username
	} else if netmail.IsFTNAddress(req.To) {
		toAddress = req.To
		toName = strings.TrimSpace(req.ToName)
		if toName == "" {
			toName = req.To
		}
	} else {
		writeError(w, http.StatusBadRequest, "unknown local user and not a valid FTN address")
		return
	}

	// See toBBSNetmailDTO's doc comment for why Subject/Body need this
	// UTF-8-to-CP437 bridge on the way in (toName is always either a
	// plain-ASCII username or FTN address, never free-typed text, so
	// it doesn't).
	m, err := s.Netmail.Send(claims.UserID, s.FTNAddress, toUserID, toName, toAddress,
		string(ansi.EncodeCP437(req.Subject)), string(ansi.EncodeCP437(wrapLongLines(req.Body, postLineWidth))), req.Crash)
	if err != nil {
		s.logWarn("could not send BBS portal netmail from %s to %s: %v", claims.Subject, req.To, err)
		writeError(w, http.StatusInternalServerError, "could not send netmail")
		return
	}
	s.logInfo("%s sent netmail %q to %s via the BBS portal", claims.Subject, m.Subject, toName)
	writeJSON(w, http.StatusCreated, toBBSNetmailDTO(*m))
}

// handleDeleteBBSNetmail permanently removes a netmail message -- the
// recipient may delete it from their Inbox, and the original sender
// may delete it from their own Sent list, mirroring
// handleGetBBSNetmail's same two-sided ownership check. There's only
// one row per message (no separate per-side copy the way an echo-area
// message's reads are tracked separately from its body), so either
// side deleting it removes it for the other side too -- a message
// with a local recipient already worked this way for the recipient's
// own delete, this just extends the same one-row-shared-by-both-sides
// reality to the sender as well, rather than inventing a soft-delete
// concept neither side had before.
func (s *Server) handleDeleteBBSNetmail(w http.ResponseWriter, r *http.Request) {
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
	m, err := s.Netmail.MessageByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "netmail message not found")
		return
	}
	isRecipient := m.ToUserID.Valid && m.ToUserID.Int64 == claims.UserID
	isSender := m.FromUserID.Valid && m.FromUserID.Int64 == claims.UserID
	if !isRecipient && !isSender {
		writeError(w, http.StatusForbidden, "not permitted to delete this message")
		return
	}
	if err := s.Netmail.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete message")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
