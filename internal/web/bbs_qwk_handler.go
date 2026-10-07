package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/qwkdoor"
)

// qwkAreaDTO describes one message area for the QWK area-selection
// UI/API -- Selected reflects message.Store.QWKSelectedAreaIDs's own
// "empty selection means everything" default, so a caller never sees
// a confusing "nothing selected" state before they've configured
// anything.
type qwkAreaDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Selected    bool   `json:"selected"`
}

// handleListBBSQWKAreas is the BBS portal's/scripts' way to see which
// areas are currently included in the caller's QWK packets.
func (s *Server) handleListBBSQWKAreas(w http.ResponseWriter, r *http.Request) {
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
	mine, err := s.Messages.InMyAreas(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load qwk area selection")
		return
	}
	dtos := make([]qwkAreaDTO, len(stats))
	for i, st := range stats {
		dtos[i] = qwkAreaDTO{
			ID:          st.Area.ID,
			Name:        st.Area.Name,
			Description: st.Area.Description,
			Selected:    mine(st.Area.ID),
		}
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleSetBBSQWKAreas sets the caller's areas (message.Store's
// "my areas", the same for the new scan and the reader) from the
// picked ones: every readable area not picked is out.
func (s *Server) handleSetBBSQWKAreas(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	var req struct {
		AreaIDs []int64 `json:"area_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	stats, err := s.Messages.ListAreaStats(claims.SecurityLevel, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list message areas")
		return
	}
	// The readable areas not picked are out; none picked is none.
	picked := make(map[int64]bool, len(req.AreaIDs))
	for _, id := range req.AreaIDs {
		picked[id] = true
	}
	var out []int64
	for _, st := range stats {
		if !picked[st.Area.ID] {
			out = append(out, st.Area.ID)
		}
	}
	if err := s.Messages.SetUnsubscribedAreas(claims.UserID, out); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save qwk area selection")
		return
	}
	s.logInfo("%s changed their areas via the BBS portal: %d of %d", claims.Subject, len(stats)-len(out), len(stats))
	w.WriteHeader(http.StatusNoContent)
}

// handleDownloadBBSQWK builds and streams the caller's current QWK
// packet -- the same qwkdoor.BuildPacketForUser/CommitRead used by the
// Telnet/SSH "qwk" builtin (internal/bbs/qwk.go), so the web portal
// and a scripted client seeing this same endpoint get identical
// content and read-state bookkeeping. Responds 204 (no body) if there
// is no new mail to include, so a script can branch on status code
// alone.
func (s *Server) handleDownloadBBSQWK(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	u, err := s.Users.ByID(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}
	bbsCfg, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load bbs config")
		return
	}

	tmpDir, err := os.MkdirTemp("", "nullmodem-qwk-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build qwk packet")
		return
	}
	defer os.RemoveAll(tmpDir)

	result, err := qwkdoor.BuildPacketForUser(s.Messages, s.Netmail, u, bbsCfg.BBS.Name, bbsCfg.BBS.Sysop, tmpDir)
	if err != nil {
		s.logWarn("building QWK packet for %s via the BBS portal: %v", claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "could not build qwk packet")
		return
	}
	if result.MessageCount == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	f, err := os.Open(result.PacketPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open qwk packet")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open qwk packet")
		return
	}

	bbsID := qwkdoor.BBSID(bbsCfg.BBS.Name)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+bbsID+`.QWK"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	// "GET" routes answer HEAD too: nothing is delivered, so nothing
	// gets marked read.
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Mark read only once the whole packet has actually gone out, the
	// same way the Telnet/SSH download waits for its Zmodem transfer to
	// finish -- an interrupted download leaves everything unread for the
	// next pull. A plain copy rather than http.ServeContent on purpose:
	// a Range request would deliver only part of the packet.
	n, err := io.Copy(w, f)
	if err == nil && n != info.Size() {
		err = io.ErrShortWrite
	}
	if err == nil {
		if ferr := http.NewResponseController(w).Flush(); ferr != nil && !errors.Is(ferr, http.ErrNotSupported) {
			err = ferr
		}
	}
	if err != nil {
		s.logWarn("QWK download for %s via the BBS portal did not complete (%d of %d bytes), messages left unread: %v", claims.Subject, n, info.Size(), err)
		return
	}

	if err := qwkdoor.CommitRead(s.Messages, s.Netmail, u.ID, result.UnreadNetmailIDs, result.MarkRead); err != nil {
		// Headers and body are already sent; the only effect is that
		// the same messages come again in the next packet.
		s.logWarn("QWK download for %s via the BBS portal: could not update read state: %v", claims.Subject, err)
		return
	}
	s.logInfo("%s downloaded a QWK packet via the BBS portal: %d message(s)", claims.Subject, result.MessageCount)
}

// handleUploadBBSQWKReply accepts a .REP reply packet (multipart form
// field "file", same convention as handleUploadBBSAreaFile) and
// routes its replies via qwkdoor.RouteReplies -- usable from the web
// portal's own upload control or scripted (e.g. curl -F
// file=@reply.rep) for automated QWK exchange.
func (s *Server) handleUploadBBSQWKReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	if s.pendingApproval(w, claims.UserID) {
		return
	}
	u, err := s.Users.ByID(claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}
	bbsCfg, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load bbs config")
		return
	}

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "upload too large or malformed")
		return
	}
	part, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file upload")
		return
	}
	defer part.Close()

	tmpDir, err := os.MkdirTemp("", "nullmodem-qwkrep-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not process reply packet")
		return
	}
	defer os.RemoveAll(tmpDir)

	repPath := filepath.Join(tmpDir, "reply.rep")
	dst, err := os.Create(repPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not process reply packet")
		return
	}
	if _, err := dst.ReadFrom(part); err != nil {
		dst.Close()
		writeError(w, http.StatusInternalServerError, "could not read reply packet")
		return
	}
	dst.Close()

	bbsID := qwkdoor.BBSID(bbsCfg.BBS.Name)
	replies, err := qwkdoor.ParseReply(repPath, bbsID)
	if err != nil {
		// The reason goes back to the caller too: an offline reader's
		// user has no other way to find out what was wrong with the
		// packet, and it names nothing but the packet's own contents.
		s.logWarn("QWK reply packet from %s via the BBS portal could not be parsed: %v", claims.Subject, err)
		reason := strings.ReplaceAll(strings.TrimPrefix(err.Error(), "qwk: "), repPath, "the packet")
		writeError(w, http.StatusBadRequest, "could not parse reply packet: "+reason)
		return
	}

	res, err := qwkdoor.RouteReplies(s.Messages, s.Netmail, s.Users, s.FTNAddress, u, replies, s.emailConfig())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not process replies")
		return
	}

	s.logInfo("%s uploaded a QWK reply packet via the BBS portal: %d posted, %d netmail sent, %d skipped", claims.Subject, res.Posted, res.Sent, len(res.Rejected))
	rejected := res.Rejected
	if rejected == nil {
		rejected = []qwkdoor.Rejected{}
	}
	// Rejected names each reply that was not delivered by its position
	// in the packet, so an offline reader can keep exactly those.
	writeJSON(w, http.StatusOK, struct {
		Posted   int                `json:"posted"`
		Sent     int                `json:"sent"`
		Skipped  int                `json:"skipped"`
		Rejected []qwkdoor.Rejected `json:"rejected"`
	}{res.Posted, res.Sent, len(res.Rejected), rejected})
}
