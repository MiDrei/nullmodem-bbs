package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/midrei/nullmodem-bbs/internal/archive"
	"github.com/midrei/nullmodem-bbs/internal/tosser"
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

type inspectedMessageDTO struct {
	OrigAddr string `json:"orig_addr"`
	DestAddr string `json:"dest_addr"`
	FromName string `json:"from_name"`
	ToName   string `json:"to_name"`
	Subject  string `json:"subject"`
	Written  string `json:"written"`
	Private  bool   `json:"private"`
	AreaTag  string `json:"area_tag"`
	BodySize int    `json:"body_size"`
}

type inspectedPacketDTO struct {
	Name     string                `json:"name"`
	OrigAddr string                `json:"orig_addr"`
	DestAddr string                `json:"dest_addr"`
	Created  string                `json:"created"`
	Messages []inspectedMessageDTO `json:"messages"`
}

type inspectedTICDTO struct {
	Area        string `json:"area"`
	File        string `json:"file"`
	Description string `json:"description"`
	SizeBytes   int64  `json:"size_bytes"`
	HasCRC32    bool   `json:"has_crc32"`
	CRC32       string `json:"crc32"`
	Origin      string `json:"origin"`
}

// handleInspectArchiveEntry parses id's raw captured bytes as an FTN
// artifact (an FTS-0001 packet, an FTS-5005 packet bundle, or an
// FTS-0006 TIC descriptor -- see tosser.Inspect) and returns a
// structured summary of what it actually contains, for the Packet
// Analyzer's detail view. Purely read-only, distinct from re-tossing.
func (s *Server) handleInspectArchiveEntry(w http.ResponseWriter, r *http.Request) {
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
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read archived file")
		return
	}

	insp, err := tosser.Inspect(entry.Filename, data)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "unknown", "error": err.Error()})
		return
	}

	resp := map[string]any{"kind": insp.Kind}
	if len(insp.Packets) > 0 {
		packets := make([]inspectedPacketDTO, len(insp.Packets))
		for i, p := range insp.Packets {
			msgs := make([]inspectedMessageDTO, len(p.Messages))
			for j, m := range p.Messages {
				msgs[j] = inspectedMessageDTO{
					OrigAddr: m.OrigAddr,
					DestAddr: m.DestAddr,
					FromName: m.FromName,
					ToName:   m.ToName,
					Subject:  m.Subject,
					Written:  m.Written.Format(time.RFC3339),
					Private:  m.Private,
					AreaTag:  m.AreaTag,
					BodySize: m.BodySize,
				}
			}
			packets[i] = inspectedPacketDTO{
				Name:     p.Name,
				OrigAddr: p.OrigAddr,
				DestAddr: p.DestAddr,
				Created:  p.Created.Format(time.RFC3339),
				Messages: msgs,
			}
		}
		resp["packets"] = packets
	}
	if insp.TIC != nil {
		crc := ""
		if insp.TIC.HasCRC32 {
			crc = fmt.Sprintf("%08x", insp.TIC.CRC32)
		}
		resp["tic"] = inspectedTICDTO{
			Area:        insp.TIC.Area,
			File:        insp.TIC.File,
			Description: insp.TIC.Description,
			SizeBytes:   insp.TIC.SizeBytes,
			HasCRC32:    insp.TIC.HasCRC32,
			CRC32:       crc,
			Origin:      insp.TIC.Origin,
		}
	}
	writeJSON(w, http.StatusOK, resp)
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
		Sysop:        c.BBS.Sysop,
		Location:     c.BBS.Location,
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
