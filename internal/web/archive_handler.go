package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"

	"git.maik.ch/swissmaik/nullmodem/internal/archive"
	"git.maik.ch/swissmaik/nullmodem/internal/tosser"
)

type archiveEntryDTO struct {
	ID            int64  `json:"id"`
	Filename      string `json:"filename"`
	UplinkAddress string `json:"uplink_address"`
	UplinkHost    string `json:"uplink_host"`
	SizeBytes     int64  `json:"size_bytes"`
	SizeHuman     string `json:"size_human"`
	ReceivedAt    string `json:"received_at"`
	// Outcome is "ok", "skipped", or "error" -- see
	// internal/tosser's handleInboundFile.
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
}

func toArchiveEntryDTO(e archive.Entry) archiveEntryDTO {
	return archiveEntryDTO{
		ID:            e.ID,
		Filename:      e.Filename,
		UplinkAddress: e.UplinkAddress,
		UplinkHost:    e.UplinkHost,
		SizeBytes:     e.SizeBytes,
		SizeHuman:     humanize.Bytes(uint64(e.SizeBytes)),
		ReceivedAt:    e.ReceivedAt.Format(time.RFC3339),
		Outcome:       e.Outcome,
		Detail:        e.Detail,
	}
}

// handleListArchive lists archived inbound files (see
// internal/archive), most recently captured first -- the web admin's
// Packet Analyzer.
func (s *Server) handleListArchive(w http.ResponseWriter, r *http.Request) {
	if s.Archive == nil {
		writeError(w, http.StatusInternalServerError, "archive is not configured")
		return
	}
	limit, offset := 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	entries, total, err := s.Archive.List(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list archive")
		return
	}
	dtos := make([]archiveEntryDTO, len(entries))
	for i, e := range entries {
		dtos[i] = toArchiveEntryDTO(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": dtos, "total": total, "limit": limit, "offset": offset})
}

// handleDownloadArchiveEntry streams id's raw captured bytes exactly
// as received -- for inspecting a file (a hex viewer, an external
// tool, ...) beyond what the admin UI itself renders inline.
func (s *Server) handleDownloadArchiveEntry(w http.ResponseWriter, r *http.Request) {
	if s.Archive == nil {
		writeError(w, http.StatusInternalServerError, "archive is not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry id")
		return
	}
	entry, err := s.Archive.ByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "archive entry not found")
		return
	}
	rc, err := s.Archive.Open(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open archived file")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Disposition", "attachment; filename=\""+entry.Filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := io.Copy(w, rc); err != nil {
		s.logWarn("streaming archived file %d: %v", id, err)
	}
}

// handleDeleteArchiveEntry permanently removes one archived entry.
func (s *Server) handleDeleteArchiveEntry(w http.ResponseWriter, r *http.Request) {
	if s.Archive == nil {
		writeError(w, http.StatusInternalServerError, "archive is not configured")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry id")
		return
	}
	if err := s.Archive.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete entry")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted archived inbound file %d", claims.Subject, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRetossArchiveEntries re-tosses one or more archived entries'
// raw bytes together, as if they'd just arrived in a single BinkP
// session -- see tosser.Retoss's own doc comment for why more than
// one entry can matter (a TIC descriptor and its payload only toss
// successfully paired together).
func (s *Server) handleRetossArchiveEntries(w http.ResponseWriter, r *http.Request) {
	if s.Archive == nil || s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "archive is not configured")
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

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}

	files := make([]tosser.RetossFile, 0, len(req.IDs))
	for _, id := range req.IDs {
		entry, err := s.Archive.ByID(id)
		if err != nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf("archive entry %d not found", id))
			return
		}
		rc, err := s.Archive.Open(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not open archive entry %d", id))
			return
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not read archive entry %d", id))
			return
		}
		files = append(files, tosser.RetossFile{
			Name:          entry.Filename,
			Data:          data,
			UplinkAddress: entry.UplinkAddress,
			UplinkHost:    entry.UplinkHost,
		})
	}

	robot := &tosser.RobotConfig{
		OurAddresses: c.BBS.FTNAddresses,
		BBSName:      c.BBS.Name,
		Uplinks:      c.Binkp.Uplinks,
		EchoStore:    s.EchoAreafix,
		FileStore:    s.FileAreafix,
		Files:        s.Files,
		Archive:      s.Archive,
	}
	ticCfg := &tosser.TICConfig{Files: s.Files}
	res, err := tosser.Retoss(files, c.Binkp.Uplinks, s.Netmail, s.Messages, s.Users, robot, ticCfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("re-toss failed: %v", err))
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s re-tossed %d archived file(s)", claims.Subject, len(req.IDs))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"received":       res.Received,
		"received_echo":  res.ReceivedEcho,
		"received_files": res.ReceivedFiles,
		"skipped_files":  res.SkippedFiles,
	})
}
