package web

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/version"
)

const previewWidth = 80

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
