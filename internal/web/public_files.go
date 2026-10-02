package web

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"
	"github.com/dustin/go-humanize"

	"git.maik.ch/nullmodem/bbs/internal/file"
)

// Files anyone may download: those in areas the sysop made public
// (file.Area.Public). Each has a page to share (/share/f/{id}, with a
// link preview) and a download without login (/dl/{id}/{name}).

type publicFileDTO struct {
	ID           int64      `json:"id"`
	Filename     string     `json:"filename"`
	Summary      string     `json:"summary"`
	Description  string     `json:"description,omitempty"`
	Preformatted bool       `json:"preformatted,omitempty"`
	Grid         *ansi.Grid `json:"grid,omitempty"`
	Size         string     `json:"size"`
	Area         string     `json:"area"`
	Network      string     `json:"network,omitempty"`
	UploadedBy   string     `json:"uploaded_by"`
	UploadedAt   string     `json:"uploaded_at"`
	Downloads    int        `json:"downloads"`
	Page         string     `json:"page"`
	Download     string     `json:"download"`
}

// publicFile is file id, if it's in a public area.
func (s *Server) publicFile(id int64) (*file.File, *file.Area, bool) {
	f, err := s.Files.FileByID(id)
	if err != nil {
		return nil, nil, false
	}
	area, err := s.Files.AreaByID(f.AreaID)
	if err != nil || !area.Public || area.Pending {
		return nil, nil, false
	}
	return f, area, true
}

func toPublicFileDTO(f *file.File, a *file.Area, full bool) publicFileDTO {
	desc := ansi.DecodeCP437([]byte(f.Description))
	summary := strings.TrimSpace(strings.SplitN(stripEscapes(desc), "\n", 2)[0])
	d := publicFileDTO{
		ID: f.ID, Filename: f.Filename, Summary: summary, Size: humanize.Bytes(uint64(f.SizeBytes)),
		Area: a.Name, Network: a.Network, UploadedBy: ansi.DecodeCP437([]byte(f.UploadedByName)),
		UploadedAt: f.UploadedAt.Format(time.RFC3339), Downloads: f.DownloadCount,
		Page:     fmt.Sprintf("/share/f/%d", f.ID),
		Download: fmt.Sprintf("/dl/%d/%s", f.ID, urlName(f.Filename)),
	}
	if full {
		d.Description = stripEscapes(desc)
		// FILE_ID.DIZ is often ANSI art: drawn like a message's.
		if ansi.IsPreformatted(f.Description) {
			g := ansi.ParseGrid(f.Description, artWidth)
			d.Preformatted, d.Grid = true, &g
		}
	}
	return d
}

func urlName(name string) string {
	return strings.NewReplacer("/", "_", "?", "_", "#", "_", " ", "%20").Replace(name)
}

// handlePublicFiles: GET /api/public/files -- the newest public files.
func (s *Server) handlePublicFiles(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	files, err := s.Files.PublicFiles(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the files")
		return
	}
	out := []publicFileDTO{}
	areas := map[int64]*file.Area{}
	for i := range files {
		a, ok := areas[files[i].AreaID]
		if !ok {
			if a, _ = s.Files.AreaByID(files[i].AreaID); a == nil {
				continue
			}
			areas[a.ID] = a
		}
		out = append(out, toPublicFileDTO(&files[i], a, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePublicFile: GET /api/public/files/{id}.
func (s *Server) handlePublicFile(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f, a, ok := s.publicFile(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no such file to share")
		return
	}
	writeJSON(w, http.StatusOK, toPublicFileDTO(f, a, true))
}

// handlePublicDownload: GET /dl/{id}/{name} -- the file, no login.
func (s *Server) handlePublicDownload(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f, _, ok := s.publicFile(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	fh, err := os.Open(f.StoragePath)
	if err != nil {
		http.Error(w, "the file is missing", http.StatusNotFound)
		return
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		http.Error(w, "the file is missing", http.StatusNotFound)
		return
	}
	// A HEAD (a link preview peeking) or a resumed range isn't a download.
	if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
		s.Files.RecordDownload(f.ID)
		s.logInfo("%s downloaded through a share link (from %s)", f.Filename, clientIP(r))
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, f.Filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, f.Filename, info.ModTime(), fh)
}

// fileShareMeta is the link preview of a shared file's page.
func (s *Server) fileShareMeta(r *http.Request, bbsName string) (title, desc string, ok bool) {
	rest, found := strings.CutPrefix(r.URL.Path, "/share/f/")
	if !found {
		return "", "", false
	}
	id, err := strconv.ParseInt(strings.Trim(rest, "/"), 10, 64)
	if err != nil {
		return "", "", false
	}
	f, a, ok := s.publicFile(id)
	if !ok {
		return "", "", false
	}
	d := toPublicFileDTO(f, a, false)
	desc = d.Summary
	if desc != "" {
		desc += " -- "
	}
	desc += fmt.Sprintf("%s, %s on %s", d.Size, a.Name, bbsName)
	return f.Filename, desc, true
}
