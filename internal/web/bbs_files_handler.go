package web

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"

	"git.maik.ch/swissmaik/nullmodem/internal/file"
)

type bbsFileAreaDTO struct {
	ID            int64  `json:"id"`
	Tag           string `json:"tag"`
	Name          string `json:"name"`
	Network       string `json:"network"`
	MinSLDownload int    `json:"min_sl_download"`
	MinSLUpload   int    `json:"min_sl_upload"`
	Total         int    `json:"total"`
	New           int    `json:"new"`
	Yours         int    `json:"yours"`
}

func toBBSFileAreaDTO(a file.AreaWithStats) bbsFileAreaDTO {
	return bbsFileAreaDTO{
		ID:            a.Area.ID,
		Tag:           a.Area.Tag,
		Name:          a.Area.Name,
		Network:       a.Area.Network,
		MinSLDownload: a.Area.MinSLDownload,
		MinSLUpload:   a.Area.MinSLUpload,
		Total:         a.Total,
		New:           a.New,
		Yours:         a.Yours,
	}
}

// handleListBBSFileAreas is handleListBBSMessageAreas' exact
// counterpart for file areas.
func (s *Server) handleListBBSFileAreas(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	stats, err := s.Files.ListAreaStats(claims.SecurityLevel, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list file areas")
		return
	}
	dtos := make([]bbsFileAreaDTO, len(stats))
	for i, st := range stats {
		dtos[i] = toBBSFileAreaDTO(st)
	}
	writeJSON(w, http.StatusOK, dtos)
}

type bbsFileDTO struct {
	ID            int64  `json:"id"`
	AreaID        int64  `json:"area_id"`
	Filename      string `json:"filename"`
	Description   string `json:"description"`
	SizeBytes     int64  `json:"size_bytes"`
	SizeHuman     string `json:"size_human"`
	UploadedBy    string `json:"uploaded_by"`
	UploadedAt    string `json:"uploaded_at"`
	DownloadCount int    `json:"download_count"`
	Unread        bool   `json:"unread"`
}

// handleListBBSAreaFiles is handleListBBSMessages' counterpart for
// files: unlike messages, real file-area sizes are small enough today
// (see docs/docker.md's migration notes) that this loads a whole area
// at once rather than paginating -- add pagination here too if that
// stops being true.
func (s *Server) handleListBBSAreaFiles(w http.ResponseWriter, r *http.Request) {
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
	area, err := s.Files.AreaByID(areaID)
	if err != nil {
		if errors.Is(err, file.ErrAreaNotFound) {
			writeError(w, http.StatusNotFound, "file area not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load file area")
		return
	}
	if !area.CanDownload(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to browse this area")
		return
	}

	files, err := s.Files.ListFiles(areaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	read, err := s.Files.ReadFileIDs(claims.UserID, areaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load read state")
		return
	}

	dtos := make([]bbsFileDTO, len(files))
	for i, f := range files {
		dtos[i] = bbsFileDTO{
			ID:            f.ID,
			AreaID:        f.AreaID,
			Filename:      f.Filename,
			Description:   f.Description,
			SizeBytes:     f.SizeBytes,
			SizeHuman:     humanize.Bytes(uint64(f.SizeBytes)),
			UploadedBy:    f.UploadedByName,
			UploadedAt:    f.UploadedAt.Format(time.RFC3339),
			DownloadCount: f.DownloadCount,
			Unread:        !read[f.ID],
		}
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleDownloadBBSFile streams a file straight over HTTP -- unlike
// internal/bbs's own Zmodem-over-Telnet download (see
// internal/bbs/files.go's downloadFile), a browser can just receive
// raw bytes with a normal Content-Disposition, so this needs no wire
// protocol at all.
func (s *Server) handleDownloadBBSFile(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}
	f, err := s.Files.FileByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	area, err := s.Files.AreaByID(f.AreaID)
	if err != nil || !area.CanDownload(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to download this file")
		return
	}

	fh, err := os.Open(f.StoragePath)
	if err != nil {
		s.logWarn("open %s for BBS portal download by %s: %v", f.StoragePath, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "could not open that file")
		return
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not stat that file")
		return
	}

	if err := s.Files.RecordDownload(f.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record download")
		return
	}
	if err := s.Files.MarkFileRead(claims.UserID, f.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not mark file read")
		return
	}
	s.logInfo("%s downloaded %s via the BBS portal", claims.Subject, f.Filename)

	w.Header().Set("Content-Disposition", "attachment; filename=\""+f.Filename+"\"")
	http.ServeContent(w, r, f.Filename, info.ModTime(), fh)
}

// handleUploadBBSAreaFile is the BBS portal's counterpart to the
// sysop admin's handleUploadAreaFile, gated by CanUpload instead of
// requiring sysop level.
func (s *Server) handleUploadBBSAreaFile(w http.ResponseWriter, r *http.Request) {
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
	area, err := s.Files.AreaByID(areaID)
	if err != nil {
		if errors.Is(err, file.ErrAreaNotFound) {
			writeError(w, http.StatusNotFound, "file area not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load file area")
		return
	}
	if !area.CanUpload(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to upload to this area")
		return
	}

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "upload too large or malformed")
		return
	}
	part, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file upload")
		return
	}
	defer part.Close()

	description := r.FormValue("description")

	f, err := s.Files.UploadFile(areaID, claims.UserID, header.Filename, description, part)
	if err != nil {
		if errors.Is(err, file.ErrDuplicateFilename) {
			writeError(w, http.StatusConflict, "a file with that name already exists in this area")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not store uploaded file")
		return
	}
	s.logInfo("%s uploaded %s (%s) into file area %d via the BBS portal", claims.Subject, f.Filename, humanize.Bytes(uint64(f.SizeBytes)), f.AreaID)
	writeJSON(w, http.StatusCreated, bbsFileDTO{
		ID:            f.ID,
		AreaID:        f.AreaID,
		Filename:      f.Filename,
		Description:   f.Description,
		SizeBytes:     f.SizeBytes,
		SizeHuman:     humanize.Bytes(uint64(f.SizeBytes)),
		UploadedBy:    f.UploadedByName,
		UploadedAt:    f.UploadedAt.Format(time.RFC3339),
		DownloadCount: f.DownloadCount,
	})
}
