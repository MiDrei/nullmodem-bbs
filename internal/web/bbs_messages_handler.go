package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
)

// defaultMessagePageSize/maxMessagePageSize bound the ?limit= query
// param on the paginated message list (see handleListBBSMessages) --
// a sysop-facing admin listing loads everything at once (see
// handleListMessageAreas), but a caller-facing one shouldn't be able
// to force an unbounded response.
const (
	defaultMessagePageSize = 25
	maxMessagePageSize     = 100
)

type bbsMessageAreaDTO struct {
	ID         int64  `json:"id"`
	Tag        string `json:"tag"`
	Name       string `json:"name"`
	Network    string `json:"network"`
	MinSLRead  int    `json:"min_sl_read"`
	MinSLWrite int    `json:"min_sl_write"`
	Total      int    `json:"total"`
	New        int    `json:"new"`
	Yours      int    `json:"yours"`
}

func toBBSMessageAreaDTO(a message.AreaWithStats) bbsMessageAreaDTO {
	return bbsMessageAreaDTO{
		ID:         a.Area.ID,
		Tag:        a.Area.Tag,
		Name:       a.Area.Name,
		Network:    a.Area.Network,
		MinSLRead:  a.Area.MinSLRead,
		MinSLWrite: a.Area.MinSLWrite,
		Total:      a.Total,
		New:        a.New,
		Yours:      a.Yours,
	}
}

// handleListBBSMessageAreas lists every area claims.SL may read, with
// per-caller Total/New/Yours counts -- the BBS portal's counterpart
// to handleListMessageAreas, which lists every area unconditionally
// for the sysop admin panel instead.
func (s *Server) handleListBBSMessageAreas(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	stats, err := s.Messages.ListAreaStats(claims.SecurityLevel, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list message areas")
		return
	}
	dtos := make([]bbsMessageAreaDTO, len(stats))
	for i, st := range stats {
		dtos[i] = toBBSMessageAreaDTO(st)
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleFirstUnreadMessagePosition reports where (in the area's own
// oldest-first order) the caller should jump to on entering areaID --
// see message.Store.FirstUnreadPosition. The frontend divides this by
// its own page size to know which page to open, mirroring how
// Telnet/SSH sessions jump the area lightbar straight to the first
// unread message on entry instead of always opening at the oldest
// page.
func (s *Server) handleFirstUnreadMessagePosition(w http.ResponseWriter, r *http.Request) {
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
		if errors.Is(err, message.ErrAreaNotFound) {
			writeError(w, http.StatusNotFound, "message area not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load message area")
		return
	}
	if !area.CanRead(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to read this area")
		return
	}
	position, err := s.Messages.FirstUnreadPosition(areaID, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not compute first unread position")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"position": position})
}

type bbsMessageSummaryDTO struct {
	ID       int64  `json:"id"`
	FromName string `json:"from_name"`
	ToName   string `json:"to_name"`
	Subject  string `json:"subject"`
	PostedAt string `json:"posted_at"`
	Unread   bool   `json:"unread"`
}

type bbsMessagePageDTO struct {
	Messages []bbsMessageSummaryDTO `json:"messages"`
	Total    int                    `json:"total"`
	Limit    int                    `json:"limit"`
	Offset   int                    `json:"offset"`
}

// handleListBBSMessages returns one page of areaID's messages, oldest
// first (matching ListMessages' own order), each flagged unread via
// ReadMessageIDs -- see ListMessagesPage's doc comment for why this
// paginates where the sysop admin panel and the Telnet/SSH session
// don't need to.
func (s *Server) handleListBBSMessages(w http.ResponseWriter, r *http.Request) {
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
		if errors.Is(err, message.ErrAreaNotFound) {
			writeError(w, http.StatusNotFound, "message area not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load message area")
		return
	}
	if !area.CanRead(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to read this area")
		return
	}

	limit := defaultMessagePageSize
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxMessagePageSize {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	msgs, total, err := s.Messages.ListMessagesPage(areaID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list messages")
		return
	}
	read, err := s.Messages.ReadMessageIDs(claims.UserID, areaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load read state")
		return
	}

	dtos := make([]bbsMessageSummaryDTO, len(msgs))
	for i, m := range msgs {
		dtos[i] = bbsMessageSummaryDTO{
			ID:       m.ID,
			FromName: ansi.DecodeCP437([]byte(m.FromName)),
			ToName:   ansi.DecodeCP437([]byte(m.ToName)),
			Subject:  ansi.DecodeCP437([]byte(m.Subject)),
			PostedAt: m.PostedAt.Format(time.RFC3339),
			Unread:   !read[m.ID],
		}
	}
	writeJSON(w, http.StatusOK, bbsMessagePageDTO{Messages: dtos, Total: total, Limit: limit, Offset: offset})
}

// artWidth is the column width echomail ANSI art/ads are authored
// for -- the standard BBS terminal, same as internal/bbs's own reader
// uses (term.Width(), typically 80) via ansi.ParseGrid. The web
// portal has no real terminal width, so it fixes on the convention
// every such piece of content already assumes.
const artWidth = 80

type bbsMessageDTO struct {
	ID       int64  `json:"id"`
	AreaID   int64  `json:"area_id"`
	FromName string `json:"from_name"`
	ToName   string `json:"to_name"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	BodyHTML string `json:"body_html"`
	// Preformatted (see ansi.IsPreformatted) flags real ANSI art or
	// aligned plain-text block art -- the frontend renders Grid as
	// authentic pixel-bitmap CP437 glyphs on a <canvas> (see
	// web/src/lib/cp437-bitmap.ts, already used by the ANSI screen
	// designer) instead of BodyHTML's CSS/font-based spans, which
	// render block-shading/line-drawing characters inconsistently
	// (gappy, misaligned) across browsers/fonts -- confirmed live. An
	// ordinary message ignores Grid entirely and renders as normal
	// prose typography instead.
	Preformatted bool       `json:"preformatted"`
	Grid         *ansi.Grid `json:"grid,omitempty"`
	PostedAt     string     `json:"posted_at"`
	// PrevID/NextID (see message.Store.Neighbors) are the message
	// immediately before/after this one in the area's own oldest-first
	// order, letting the reader step to the next/previous message
	// without going back to the area's list first. Only set by
	// handleGetBBSMessage, which alone knows the area context to
	// compute them from -- nil at the first/last message either way.
	PrevID *int64 `json:"prev_id,omitempty"`
	NextID *int64 `json:"next_id,omitempty"`
}

// toBBSMessageDTO renders m for the BBS portal's JSON API. m.Body
// (and FromName/ToName/Subject) are raw CP437 bytes on disk -- the
// same convention Telnet/SSH sessions read and write directly (see
// internal/ansi's package doc comment) -- which aren't necessarily
// valid UTF-8 on their own, so encoding/json can't serialize them
// as-is. BodyHTML uses ansi.ToHTML directly on those raw bytes (it
// already expects that encoding, same as the ANSI screen designer's
// preview); the plain Body/FromName/ToName/Subject fields go through
// ansi.DecodeCP437 first to become safe, displayable UTF-8 text for
// a client that just wants the words (e.g. pre-filling a reply
// subject). See handlePostBBSMessage's matching ansi.EncodeCP437 on
// the way in.
func toBBSMessageDTO(m message.Message) bbsMessageDTO {
	// SEEN-BY/PATH lines are FTS-0004 routing/dupe-detection metadata
	// every tosser along the way appends -- meant for tossers, never
	// readers (see StripSeenByAndPathForDisplay's own doc comment);
	// internal/bbs's own Telnet/SSH reader already hides them the same
	// way.
	body := message.StripSeenByAndPathForDisplay(m.Body)
	preformatted := ansi.IsPreformatted(body)
	var grid *ansi.Grid
	if preformatted {
		g := ansi.ParseGrid(body, artWidth)
		grid = &g
	}
	return bbsMessageDTO{
		ID:           m.ID,
		AreaID:       m.AreaID,
		FromName:     ansi.DecodeCP437([]byte(m.FromName)),
		ToName:       ansi.DecodeCP437([]byte(m.ToName)),
		Subject:      ansi.DecodeCP437([]byte(m.Subject)),
		Body:         ansi.DecodeCP437([]byte(body)),
		BodyHTML:     ansi.ToHTML(body),
		Preformatted: preformatted,
		Grid:         grid,
		PostedAt:     m.PostedAt.Format(time.RFC3339),
	}
}

// handleGetBBSMessage loads one message and marks it read for the
// caller -- the area-level CanRead check already happened when the
// caller fetched the area's message list, so this only needs to
// confirm the message actually belongs to an area claims.SL may read
// (a caller could otherwise probe another area's message by ID).
func (s *Server) handleGetBBSMessage(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Messages.MarkMessageRead(claims.UserID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not mark message read")
		return
	}
	dto := toBBSMessageDTO(*m)
	if prev, next, err := s.Messages.Neighbors(m.AreaID, m.ID); err == nil {
		dto.PrevID, dto.NextID = prev, next
	}
	writeJSON(w, http.StatusOK, dto)
}

// handlePostBBSMessage posts a new message to areaID as the logged-in
// caller.
func (s *Server) handlePostBBSMessage(w http.ResponseWriter, r *http.Request) {
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
		if errors.Is(err, message.ErrAreaNotFound) {
			writeError(w, http.StatusNotFound, "message area not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load message area")
		return
	}
	if !area.CanWrite(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to post in this area")
		return
	}

	var req struct {
		ToName  string `json:"to_name"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Subject == "" || req.Body == "" {
		writeError(w, http.StatusBadRequest, "subject and body are required")
		return
	}
	toName := req.ToName
	if toName == "" {
		toName = "All"
	}

	// A browser sends UTF-8 (a German "ä" is 2 bytes); this project's
	// message store and its Telnet/SSH readers/ToHTML all expect raw
	// CP437 bytes (1 byte per glyph) instead -- see toBBSMessageDTO's
	// doc comment for the read side of this same bridge. Without this,
	// a non-ASCII character round-trips as CP437-decoded garbage
	// (confirmed live: "ä" became "├ñ").
	m, err := s.Messages.PostMessage(areaID, claims.UserID,
		string(ansi.EncodeCP437(toName)), string(ansi.EncodeCP437(req.Subject)), string(ansi.EncodeCP437(req.Body)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not post message")
		return
	}
	s.logInfo("%s posted %q to message area %d via the BBS portal", claims.Subject, m.Subject, areaID)
	writeJSON(w, http.StatusCreated, toBBSMessageDTO(*m))
}
