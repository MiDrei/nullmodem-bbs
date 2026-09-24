package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/version"
)

const previewWidth = 80

const (
	defaultDesignerWidth  = 80
	defaultDesignerHeight = 25
	maxDesignerWidth      = 240
	maxDesignerHeight     = 500
	maxImportBytes        = 1 << 20 // 1 MiB is generous for a text-mode .ans file
)

// screenNamePattern restricts screen file names the same way
// areaTagPattern restricts area tags: predictable characters only,
// since the name becomes an on-disk file name directly.
var screenNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+\.ans$`)

type screenSummaryDTO struct {
	Name string `json:"name"`
}

// handleListScreens lists the .ans screen files available for preview
// in the configured screens directory (see internal/ansi.LoadScreen).
func (s *Server) handleListScreens(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	entries, err := os.ReadDir(c.BBS.ScreensDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list screens")
		return
	}
	screens := make([]screenSummaryDTO, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".ans" {
			continue
		}
		screens = append(screens, screenSummaryDTO{Name: e.Name()})
	}
	writeJSON(w, http.StatusOK, screens)
}

type screenPreviewDTO struct {
	Name string `json:"name"`
	HTML string `json:"html"`
}

// previewVars fills in the same placeholders a live session would
// (see bbs.Server.baseVars/userVars) with representative sample
// values, since a web preview has no real node, caller or clock tied
// to a session.
func previewVars(bbsName, sysop string) ansi.Vars {
	now := time.Now()
	return ansi.Vars{
		"BBSNAME":    bbsName,
		"SYSOP":      sysop,
		"VERSION":    version.Version,
		"NODE":       "1",
		"DATE":       now.Format("2006-01-02"),
		"TIME":       now.Format("15:04:05"),
		"USERNAME":   "previewuser",
		"SL":         "10",
		"TOTALCALLS": "1",
	}
}

// handlePreviewScreen renders one screen file to HTML for the web
// admin's live preview. filepath.Base guards against a name like
// "../../etc/passwd" escaping the configured screens directory.
func (s *Server) handlePreviewScreen(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}

	raw, err := ansi.LoadScreen(filepath.Join(c.BBS.ScreensDir, name))
	if err != nil {
		writeError(w, http.StatusNotFound, "screen not found")
		return
	}

	rendered := ansi.Render(raw, previewVars(c.BBS.Name, c.BBS.Sysop))
	rendered = ansi.Layout(rendered, previewWidth)
	writeJSON(w, http.StatusOK, screenPreviewDTO{Name: name, HTML: ansi.ToHTML(rendered)})
}

type gridDTO struct {
	Width  int         `json:"width"`
	Height int         `json:"height"`
	Cells  []ansi.Cell `json:"cells"`
}

func toGridDTO(g ansi.Grid) gridDTO {
	return gridDTO{Width: g.Width, Height: g.Height, Cells: g.Cells}
}

func (d gridDTO) toGrid() ansi.Grid {
	return ansi.Grid{Width: d.Width, Height: d.Height, Cells: d.Cells}
}

// handleGetScreenGrid parses an existing screen file into the grid
// model the designer edits, so opening a screen for editing starts
// from what's actually on disk (including one created outside the
// designer, or a raw .ans imported via handleImportScreen).
func (s *Server) handleGetScreenGrid(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	raw, err := ansi.LoadScreen(filepath.Join(c.BBS.ScreensDir, name))
	if err != nil {
		writeError(w, http.StatusNotFound, "screen not found")
		return
	}
	writeJSON(w, http.StatusOK, toGridDTO(ansi.ParseGrid(raw, inferWidth(raw))))
}

// inferWidth returns the column width to open a raw .ans screen at
// for editing: the length of its widest line, ignoring ANSI escapes.
//
// A screen built around {FILL:x}/{PLACEHOLDER} tokens (like the
// shipped welcome.ans) has short literal source lines that only
// reach their real on-screen width -- 80, the standard BBS terminal
// -- once Render/Layout expand those tokens at connect time, so
// measuring the raw file alone would open it in a canvas far
// narrower than what callers actually see. Detecting a literal '{'
// floors the width to 80 in that case; a plain screen with no such
// tokens (including one the designer itself created at a smaller
// size) keeps its own measured width untouched.
func inferWidth(raw string) int {
	width := 0
	for _, line := range strings.Split(raw, "\r\n") {
		if w := ansi.VisibleWidth(line); w > width {
			width = w
		}
	}
	if width == 0 {
		width = defaultDesignerWidth
	}
	if strings.Contains(raw, "{") && width < defaultDesignerWidth {
		width = defaultDesignerWidth
	}
	return width
}

// handleSaveScreenGrid persists the designer's in-memory grid back to
// the screen's .ans file, encoded to raw CP437/ANSI bytes.
func (s *Server) handleSaveScreenGrid(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if !screenNamePattern.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid screen name")
		return
	}

	var dto gridDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateGridDTO(dto); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	path := filepath.Join(c.BBS.ScreensDir, name)
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, "screen not found")
		return
	}

	if err := os.WriteFile(path, []byte(dto.toGrid().Encode()), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save screen")
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s saved screen %q via the ANSI designer", claims.Subject, name)
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func validateGridDTO(dto gridDTO) string {
	if dto.Width <= 0 || dto.Width > maxDesignerWidth {
		return "width must be between 1 and 240"
	}
	if dto.Height <= 0 || dto.Height > maxDesignerHeight {
		return "height must be between 1 and 500"
	}
	if len(dto.Cells) != dto.Width*dto.Height {
		return "cells length must equal width*height"
	}
	return ""
}

// handleCreateScreen creates a new blank .ans screen file so the
// designer has something to open and start drawing on.
func (s *Server) handleCreateScreen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !screenNamePattern.MatchString(body.Name) {
		writeError(w, http.StatusBadRequest, "name must match [a-zA-Z0-9_-]+.ans")
		return
	}
	width, height := body.Width, body.Height
	if width == 0 {
		width = defaultDesignerWidth
	}
	if height == 0 {
		height = defaultDesignerHeight
	}
	if msg := validateGridDTO(gridDTO{Width: width, Height: height, Cells: make([]ansi.Cell, width*height)}); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	path := filepath.Join(c.BBS.ScreensDir, body.Name)
	if _, err := os.Stat(path); err == nil {
		writeError(w, http.StatusConflict, "a screen with that name already exists")
		return
	}

	grid := ansi.NewGrid(width, height)
	if err := os.WriteFile(path, []byte(grid.Encode()), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create screen")
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s created screen %q (%dx%d)", claims.Subject, body.Name, width, height)
	}
	writeJSON(w, http.StatusCreated, screenSummaryDTO{Name: body.Name})
}

// handleImportScreen accepts a raw .ans file upload (multipart field
// "file", optional "name" field to save it under a different name)
// and stores it as-is in the screens directory, ready to open and
// continue editing in the designer.
func (s *Server) handleImportScreen(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		writeError(w, http.StatusBadRequest, "upload too large or malformed")
		return
	}
	part, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file upload")
		return
	}
	defer part.Close()

	name := r.FormValue("name")
	if name == "" {
		name = header.Filename
	}
	if !screenNamePattern.MatchString(name) {
		writeError(w, http.StatusBadRequest, "name must match [a-zA-Z0-9_-]+.ans")
		return
	}

	data, err := io.ReadAll(io.LimitReader(part, maxImportBytes+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read upload")
		return
	}
	if len(data) > maxImportBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	path := filepath.Join(c.BBS.ScreensDir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "could not store uploaded screen")
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s imported screen %q via the ANSI designer", claims.Subject, name)
	}
	writeJSON(w, http.StatusCreated, screenSummaryDTO{Name: name})
}

// handleDeleteScreen removes a screen file from the screens directory.
func (s *Server) handleDeleteScreen(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	path := filepath.Join(c.BBS.ScreensDir, name)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "screen not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete screen")
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted screen %q", claims.Subject, name)
	}
	w.WriteHeader(http.StatusNoContent)
}
