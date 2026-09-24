package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"git.maik.ch/nullmodem/kit/qwk"
	"git.maik.ch/swissmaik/nullmodem/internal/qwkdoor"
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
	selected, err := s.Messages.QWKSelectedAreaIDs(claims.UserID)
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
			Selected:    len(selected) == 0 || selected[st.Area.ID],
		}
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleSetBBSQWKAreas replaces the caller's QWK area selection.
// Selecting every readable area (or none at all) is stored as "no
// selection" -- see message.Store.SetQWKSelectedAreas -- since both
// mean the same thing: include everything.
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
	readable := make(map[int64]bool, len(stats))
	for _, st := range stats {
		readable[st.Area.ID] = true
	}
	var ids []int64
	for _, id := range req.AreaIDs {
		if readable[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == len(stats) {
		ids = nil // every readable area selected == no explicit selection
	}

	if err := s.Messages.SetQWKSelectedAreas(claims.UserID, ids); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save qwk area selection")
		return
	}
	s.logInfo("%s updated their QWK area selection via the BBS portal: %d area(s)", claims.Subject, len(ids))
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

	if err := qwkdoor.CommitRead(s.Messages, s.Netmail, u.ID, result.UnreadNetmailIDs, result.MarkRead); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update read state")
		return
	}

	s.logInfo("%s downloaded a QWK packet via the BBS portal: %d message(s)", claims.Subject, result.MessageCount)
	bbsID := qwkdoor.BBSID(bbsCfg.BBS.Name)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+bbsID+`.QWK"`)
	http.ServeContent(w, r, bbsID+".QWK", time.Now(), f)
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
	replies, err := qwk.ParseReplyPacket(repPath, bbsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not parse reply packet")
		return
	}

	posted, sent, skipped, err := qwkdoor.RouteReplies(s.Messages, s.Netmail, s.Users, s.FTNAddress, u, replies)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not process replies")
		return
	}

	s.logInfo("%s uploaded a QWK reply packet via the BBS portal: %d posted, %d netmail sent, %d skipped", claims.Subject, posted, sent, skipped)
	writeJSON(w, http.StatusOK, struct {
		Posted  int `json:"posted"`
		Sent    int `json:"sent"`
		Skipped int `json:"skipped"`
	}{posted, sent, skipped})
}
