package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"

	"git.maik.ch/swissmaik/nullmodem/internal/file"
)

const maxUploadBytes = 100 << 20 // 100 MiB, a sane cap for a BBS file library

type fileAreaDTO struct {
	ID            int64  `json:"id"`
	Tag           string `json:"tag"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Network       string `json:"network"`
	MinSLDownload int    `json:"min_sl_download"`
	MinSLUpload   int    `json:"min_sl_upload"`
	SortOrder     int    `json:"sort_order"`
}

func toFileAreaDTO(a file.Area) fileAreaDTO {
	return fileAreaDTO{
		ID:            a.ID,
		Tag:           a.Tag,
		Name:          a.Name,
		Description:   a.Description,
		Network:       a.Network,
		MinSLDownload: a.MinSLDownload,
		MinSLUpload:   a.MinSLUpload,
		SortOrder:     a.SortOrder,
	}
}

type fileDTO struct {
	ID            int64  `json:"id"`
	AreaID        int64  `json:"area_id"`
	Filename      string `json:"filename"`
	Description   string `json:"description"`
	SizeBytes     int64  `json:"size_bytes"`
	SizeHuman     string `json:"size_human"`
	UploadedBy    string `json:"uploaded_by"`
	UploadedAt    string `json:"uploaded_at"`
	DownloadCount int    `json:"download_count"`
}

func toFileDTO(f file.File) fileDTO {
	return fileDTO{
		ID:            f.ID,
		AreaID:        f.AreaID,
		Filename:      f.Filename,
		Description:   f.Description,
		SizeBytes:     f.SizeBytes,
		SizeHuman:     humanize.Bytes(uint64(f.SizeBytes)),
		UploadedBy:    f.UploadedByName,
		UploadedAt:    f.UploadedAt.Format(time.RFC3339),
		DownloadCount: f.DownloadCount,
	}
}

func (s *Server) handleListFileAreas(w http.ResponseWriter, r *http.Request) {
	areas, err := s.Files.AllAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list file areas")
		return
	}
	dtos := make([]fileAreaDTO, len(areas))
	for i, a := range areas {
		dtos[i] = toFileAreaDTO(a)
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (s *Server) handleCreateFileArea(w http.ResponseWriter, r *http.Request) {
	var dto fileAreaDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateAreaDTO(true, dto.Tag, dto.Name, dto.MinSLDownload, dto.MinSLUpload); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	area, err := s.Files.CreateArea(dto.Tag, dto.Name, dto.Description, dto.Network, dto.MinSLDownload, dto.MinSLUpload)
	if err != nil {
		if errors.Is(err, file.ErrTagTaken) {
			writeError(w, http.StatusConflict, "that tag is already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create file area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s created file area %q (%s) via web", claims.Subject, area.Name, area.Tag)
	}
	writeJSON(w, http.StatusCreated, toFileAreaDTO(*area))
}

func (s *Server) handleUpdateFileArea(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	var dto fileAreaDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateAreaDTO(false, dto.Tag, dto.Name, dto.MinSLDownload, dto.MinSLUpload); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	area, err := s.Files.UpdateArea(id, dto.Name, dto.Description, dto.Network, dto.MinSLDownload, dto.MinSLUpload, dto.SortOrder)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update file area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s updated file area %q (%s)", claims.Subject, area.Name, area.Tag)
	}
	writeJSON(w, http.StatusOK, toFileAreaDTO(*area))
}

func (s *Server) handleDeleteFileArea(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	if err := s.Files.DeleteArea(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete file area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted file area %d", claims.Subject, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAreaFiles(w http.ResponseWriter, r *http.Request) {
	areaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	files, err := s.Files.ListFiles(areaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	dtos := make([]fileDTO, len(files))
	for i, f := range files {
		dtos[i] = toFileDTO(f)
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleUploadAreaFile accepts a multipart/form-data upload with a
// "file" part (and an optional "description" field) and stores it in
// the given area. Unlike a telnet/SSH session, a browser can send
// bytes directly, so this needs no server-side-path workaround (see
// file.Store.UploadFile's doc comment).
func (s *Server) handleUploadAreaFile(w http.ResponseWriter, r *http.Request) {
	areaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	uploader, err := s.Users.ByUsername(claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve uploader")
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

	f, err := s.Files.UploadFile(areaID, uploader.ID, header.Filename, description, part)
	if err != nil {
		if errors.Is(err, file.ErrDuplicateFilename) {
			writeError(w, http.StatusConflict, "a file with that name already exists in this area")
			return
		}
		if errors.Is(err, file.ErrAreaNotFound) {
			writeError(w, http.StatusNotFound, "file area not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not store uploaded file")
		return
	}
	s.logInfo("%s uploaded %s (%s) into file area %d via web", uploader.Username, f.Filename, humanize.Bytes(uint64(f.SizeBytes)), f.AreaID)
	writeJSON(w, http.StatusCreated, toFileDTO(*f))
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}
	if err := s.Files.DeleteFile(id); err != nil {
		if errors.Is(err, file.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete file")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted file %d", claims.Subject, id)
	}
	w.WriteHeader(http.StatusNoContent)
}
