package web

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-kit/ansi"
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
	// SharePage: the file's public page, when its area is public.
	SharePage string `json:"share_page,omitempty"`
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

// handleGetBBSFile loads one file's metadata -- including its full
// (possibly multi-line, see internal/tic.File.Description) TIC/upload
// description -- and marks it read, the same way opening a message
// does. The only other way to mark a file read is downloading it (see
// handleDownloadBBSFile), which isn't always what a caller wants just
// to see what a file actually is.
func (s *Server) handleGetBBSFile(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusForbidden, "not permitted to view this file")
		return
	}
	if err := s.Files.MarkFileRead(claims.UserID, f.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not mark file read")
		return
	}
	writeJSON(w, http.StatusOK, bbsFileDTO{
		ID:            f.ID,
		AreaID:        f.AreaID,
		Filename:      f.Filename,
		Description:   f.Description,
		SizeBytes:     f.SizeBytes,
		SizeHuman:     humanize.Bytes(uint64(f.SizeBytes)),
		UploadedBy:    f.UploadedByName,
		UploadedAt:    f.UploadedAt.Format(time.RFC3339),
		DownloadCount: f.DownloadCount,
		SharePage:     sharePage(area, f.ID),
	})
}

func sharePage(a *file.Area, id int64) string {
	if a == nil || !a.Public {
		return ""
	}
	return fmt.Sprintf("/share/f/%d", id)
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
	if s.pendingApproval(w, claims.UserID) {
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

// filePreviewTextCap bounds how much of a text-shaped file
// handlePreviewBBSFile reads for a preview -- generous for a real
// FILE_ID.DIZ/NFO/ad, but a mislabeled huge .txt shouldn't blow up the
// response.
const filePreviewTextCap = 200 * 1024

// filePreviewArchiveEntryCap bounds how many of a .zip's entries
// handlePreviewBBSFile lists -- a real file-area .zip is a handful of
// files; this is only to stop something pathological from producing
// an enormous response.
const filePreviewArchiveEntryCap = 500

// previewImageExtensions maps a lowercased file extension to the
// image/* content type handlePreviewRawBBSFile serves it as -- the
// set of formats every mainstream browser renders natively in an
// <img>, so no server-side conversion is ever needed.
var previewImageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// previewTextExtensions is what handlePreviewBBSFile treats as CP437
// text worth rendering inline -- the small sidecar/description/ad
// files a file area routinely carries alongside the real payload,
// plus real ANSI art (.ans), rendered the same way an echomail
// message body already is (see toBBSMessageDTO).
var previewTextExtensions = map[string]bool{
	".txt": true, ".nfo": true, ".diz": true, ".asc": true,
	".me": true, ".1st": true, ".cat": true, ".ans": true,
}

type filePreviewArchiveEntryDTO struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// filePreviewDTO is a discriminated union keyed by Kind ("image",
// "text", "archive", or "none" when nothing about the file is worth
// previewing) -- only the fields relevant to that Kind are set.
type filePreviewDTO struct {
	Kind string `json:"kind"`
	// text kind: same shape/meaning as bbsMessageDTO's own
	// BodyHTML/Preformatted/Grid (see toBBSMessageDTO's doc comment).
	BodyHTML     string     `json:"body_html,omitempty"`
	Preformatted bool       `json:"preformatted,omitempty"`
	Grid         *ansi.Grid `json:"grid,omitempty"`
	Truncated    bool       `json:"truncated,omitempty"`
	// image kind.
	ContentType string `json:"content_type,omitempty"`
	// archive kind.
	Entries          []filePreviewArchiveEntryDTO `json:"entries,omitempty"`
	EntriesTruncated bool                         `json:"entries_truncated,omitempty"`
}

// handlePreviewBBSFile inspects one file's actual bytes (by extension
// -- an image, CP437 text/ANSI art, or a .zip's table of contents) and
// returns a structured summary for the portal's file detail page to
// render inline, without a full download. Read-only: doesn't record a
// download or change read state (handleGetBBSFile, loaded alongside
// this on the same page, already does the latter).
func (s *Server) handlePreviewBBSFile(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusForbidden, "not permitted to view this file")
		return
	}

	ext := strings.ToLower(filepath.Ext(f.Filename))
	switch {
	case previewImageExtensions[ext] != "":
		writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "image", ContentType: previewImageExtensions[ext]})

	case previewTextExtensions[ext]:
		data, truncated, err := readCapped(f.StoragePath, filePreviewTextCap)
		if err != nil {
			s.logWarn("reading %s for preview: %v", f.StoragePath, err)
			writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "none"})
			return
		}
		body := string(data)
		preformatted := ansi.IsPreformatted(body)
		var grid *ansi.Grid
		if preformatted {
			g := ansi.ParseGrid(body, artWidth)
			grid = &g
		}
		writeJSON(w, http.StatusOK, filePreviewDTO{
			Kind:         "text",
			BodyHTML:     ansi.ToHTML(body),
			Preformatted: preformatted,
			Grid:         grid,
			Truncated:    truncated,
		})

	case ext == ".zip":
		entries, truncated, err := listZipEntries(f.StoragePath, filePreviewArchiveEntryCap)
		if err != nil {
			writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "none"})
			return
		}
		writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "archive", Entries: entries, EntriesTruncated: truncated})

	default:
		writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "none"})
	}
}

// handlePreviewBBSFileEntry mirrors handlePreviewBBSFile for one
// entry inside a .zip file (identified by the exact name a "archive"
// kind preview's Entries lists) -- lets a file like fsxNet's daily
// apodNNNN.zip (an image bundled inside a .zip, not a raw image file)
// get its own inline preview without ever extracting the archive to
// disk. Query parameter "name" selects the entry.
func (s *Server) handlePreviewBBSFileEntry(w http.ResponseWriter, r *http.Request) {
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
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing name")
		return
	}
	f, err := s.Files.FileByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	area, err := s.Files.AreaByID(f.AreaID)
	if err != nil || !area.CanDownload(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to view this file")
		return
	}

	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case previewImageExtensions[ext] != "":
		writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "image", ContentType: previewImageExtensions[ext]})

	case previewTextExtensions[ext]:
		data, truncated, err := readZipEntryCapped(f.StoragePath, name, filePreviewTextCap)
		if err != nil {
			writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "none"})
			return
		}
		body := string(data)
		preformatted := ansi.IsPreformatted(body)
		var grid *ansi.Grid
		if preformatted {
			g := ansi.ParseGrid(body, artWidth)
			grid = &g
		}
		writeJSON(w, http.StatusOK, filePreviewDTO{
			Kind:         "text",
			BodyHTML:     ansi.ToHTML(body),
			Preformatted: preformatted,
			Grid:         grid,
			Truncated:    truncated,
		})

	default:
		writeJSON(w, http.StatusOK, filePreviewDTO{Kind: "none"})
	}
}

// readZipEntryCapped mirrors readCapped for one named entry inside a
// .zip file, streaming its decompressed bytes without ever writing
// them to disk.
func readZipEntryCapped(zipPath, entryName string, limit int64) (data []byte, truncated bool, err error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, false, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name != entryName {
			continue
		}
		rc, openErr := zf.Open()
		if openErr != nil {
			return nil, false, openErr
		}
		defer rc.Close()
		data, err = io.ReadAll(io.LimitReader(rc, limit+1))
		if err != nil {
			return nil, false, err
		}
		if int64(len(data)) > limit {
			return data[:limit], true, nil
		}
		return data, false, nil
	}
	return nil, false, os.ErrNotExist
}

// handlePreviewRawBBSFileEntry is handlePreviewRawBBSFile's
// counterpart for one image entry inside a .zip -- streams it
// decompressed straight from the archive, inline, no temp file.
func (s *Server) handlePreviewRawBBSFileEntry(w http.ResponseWriter, r *http.Request) {
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
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing name")
		return
	}
	f, err := s.Files.FileByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	area, err := s.Files.AreaByID(f.AreaID)
	if err != nil || !area.CanDownload(claims.SecurityLevel) {
		writeError(w, http.StatusForbidden, "not permitted to view this file")
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	contentType, ok := previewImageExtensions[ext]
	if !ok {
		writeError(w, http.StatusNotFound, "no inline preview for this file type")
		return
	}

	zr, err := zip.OpenReader(f.StoragePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open that archive")
		return
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name != name {
			continue
		}
		rc, openErr := zf.Open()
		if openErr != nil {
			writeError(w, http.StatusInternalServerError, "could not open that entry")
			return
		}
		defer rc.Close()
		w.Header().Set("Content-Type", contentType)
		if _, err := io.Copy(w, rc); err != nil {
			s.logWarn("streaming zip entry %s from %s: %v", name, f.StoragePath, err)
		}
		return
	}
	writeError(w, http.StatusNotFound, "entry not found")
}

// readCapped reads at most limit+1 bytes from path, reporting whether
// the file actually had more than limit bytes (truncated) rather than
// silently returning a partial read indistinguishable from the whole
// file.
func readCapped(path string, limit int64) (data []byte, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

// listZipEntries reads path's central directory (no decompression --
// archive/zip.OpenReader only parses the table of contents) and
// returns up to limit non-directory entries' names and uncompressed
// sizes.
func listZipEntries(path string, limit int) (entries []filePreviewArchiveEntryDTO, truncated bool, err error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, false, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		if len(entries) >= limit {
			truncated = true
			break
		}
		entries = append(entries, filePreviewArchiveEntryDTO{Name: zf.Name, SizeBytes: int64(zf.UncompressedSize64)})
	}
	return entries, truncated, nil
}

// handlePreviewRawBBSFile streams an image file's raw bytes inline
// (no Content-Disposition, so a browser displays rather than saves
// it) for handlePreviewBBSFile's "image" kind to fetch into an <img>
// -- unlike handleDownloadBBSFile, this doesn't record a download or
// change read state, since previewing an image inline isn't "getting"
// the file the way an explicit Download click is.
func (s *Server) handlePreviewRawBBSFile(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusForbidden, "not permitted to view this file")
		return
	}
	ext := strings.ToLower(filepath.Ext(f.Filename))
	contentType, ok := previewImageExtensions[ext]
	if !ok {
		writeError(w, http.StatusNotFound, "no inline preview for this file type")
		return
	}

	fh, err := os.Open(f.StoragePath)
	if err != nil {
		s.logWarn("open %s for BBS portal preview by %s: %v", f.StoragePath, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "could not open that file")
		return
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not stat that file")
		return
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, f.Filename, info.ModTime(), fh)
}
