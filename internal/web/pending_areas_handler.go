package web

import (
	"net/http"
	"strconv"
)

// pendingAreasDTO bundles both kinds of area a sysop might need to
// review in one response, since the web admin's Pending Areas page
// shows both together (see the ask behind message.Area.Pending's doc
// comment: internal/tosser's echomail toss lands new areas here
// first, invisible in the BBS until approved -- and the same
// mechanism is reserved for file areas once TIC/file-echo tossing
// exists).
type pendingAreasDTO struct {
	MessageAreas []messageAreaDTO `json:"message_areas"`
	FileAreas    []fileAreaDTO    `json:"file_areas"`
}

func (s *Server) handleListPendingAreas(w http.ResponseWriter, r *http.Request) {
	msgAreas, err := s.Messages.PendingAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list pending message areas")
		return
	}
	fileAreas, err := s.Files.PendingAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list pending file areas")
		return
	}

	msgDTOs := make([]messageAreaDTO, len(msgAreas))
	for i, a := range msgAreas {
		msgDTOs[i] = toMessageAreaDTO(a)
	}
	fileDTOs := make([]fileAreaDTO, len(fileAreas))
	for i, a := range fileAreas {
		fileDTOs[i] = toFileAreaDTO(a)
	}
	writeJSON(w, http.StatusOK, pendingAreasDTO{MessageAreas: msgDTOs, FileAreas: fileDTOs})
}

// handleApprovePendingMessageArea clears a message area's Pending
// flag, making it visible in the normal Message Areas admin list and
// every BBS-facing listing. Rejecting a pending area instead reuses
// the existing DELETE /api/message-areas/{id}.
func (s *Server) handleApprovePendingMessageArea(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	if err := s.Messages.ApproveArea(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not approve message area")
		return
	}
	area, err := s.Messages.AreaByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load approved message area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s approved message area %q (%s)", claims.Subject, area.Name, area.Tag)
	}
	writeJSON(w, http.StatusOK, toMessageAreaDTO(*area))
}

// handleApprovePendingFileArea mirrors
// handleApprovePendingMessageArea for file areas -- unused by
// anything today (no TIC/file-echo tossing creates a pending file
// area yet), kept symmetric for when it does.
func (s *Server) handleApprovePendingFileArea(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	if err := s.Files.ApproveArea(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not approve file area")
		return
	}
	area, err := s.Files.AreaByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load approved file area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s approved file area %q (%s)", claims.Subject, area.Name, area.Tag)
	}
	writeJSON(w, http.StatusOK, toFileAreaDTO(*area))
}
