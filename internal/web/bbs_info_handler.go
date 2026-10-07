package web

import (
	"net"
	"net/http"
	"os"
	"path/filepath"

	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/version"
	"git.maik.ch/nullmodem/kit/ansi"
)

// handleBBSInfo reports the BBS's own configured display name --
// deliberately unauthenticated (no requireAuth/requireBBSUser), since
// both the admin login page and the BBS portal login page need to
// show it before anyone has a token, and it's no more sensitive than
// what the Telnet/SSH welcome screen already shows any caller before
// they log in.
func (s *Server) handleBBSInfo(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	info := bbsInfoDTO{Name: c.BBS.Name, Version: version.Short(), Build: version.Build()}
	if c.Telnet.Enabled {
		info.TelnetPort = portOf(c.Telnet.Addr)
	}
	if c.SSH.Enabled {
		info.SSHPort = portOf(c.SSH.Addr)
	}
	writeJSON(w, http.StatusOK, info)
}

// bbsInfoDTO is what the portal and admin show before login: the
// board's name, its software version and where to reach it by Telnet
// and SSH (the page's own host, these ports). A port is empty when
// that server is disabled.
type bbsInfoDTO struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Build is the commit and build time, "" when unknown.
	Build      string `json:"build,omitempty"`
	TelnetPort string `json:"telnet_port,omitempty"`
	SSHPort    string `json:"ssh_port,omitempty"`
}

// portOf is the port part of a listen address like ":2323" or
// "0.0.0.0:2323".
func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return ""
}

// welcomeScreenFile is the same fixed, convention-based filename
// cmd/bbs always loads for the Telnet/SSH connect banner (see
// internal/bbs/session.go's logoffScreenFile doc comment for the
// sibling convention) -- shown here too so the portal login page can
// carry the same first impression.
const welcomeScreenFile = "welcome.ans"

// welcomeScreenDTO mirrors bbsMessageDTO's own
// BodyHTML/Preformatted/Grid shape (see toBBSMessageDTO's doc
// comment): real ANSI/CP437 art (Preformatted, with Grid set) must
// render via the AnsiArt canvas component, never as HTML/font-based
// spans -- confirmed live that a browser font renders CP437 block-
// shading and line-drawing glyphs inconsistently. HTML is still sent
// as a plain-text fallback for a welcome.ans that isn't real
// preformatted art.
type welcomeScreenDTO struct {
	HTML         string     `json:"html"`
	Preformatted bool       `json:"preformatted"`
	Grid         *ansi.Grid `json:"grid,omitempty"`
}

// handleWelcomeScreen renders welcome.ans for the BBS portal login
// page -- deliberately unauthenticated, for the same reason as
// handleBBSInfo, and reusing handlePreviewScreen's own rendering
// pipeline (ansi.Render/Layout with previewVars' placeholder values,
// since there's no real session yet to fill NODE/USERNAME/SL/
// TOTALCALLS from). A missing welcome.ans (never created, or deleted)
// is reported as 404 rather than an error -- the portal login page
// simply has no banner to show, not broken.
func (s *Server) handleWelcomeScreen(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	// In the visitor's language where there's one (welcome.de.ans).
	lang := s.requestLang(r)
	raw, err := "", os.ErrNotExist
	for _, name := range i18n.ScreenNames(welcomeScreenFile, lang) {
		if raw, err = ansi.LoadScreen(filepath.Join(c.BBS.ScreensDir, name)); err == nil {
			break
		}
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "no welcome screen configured")
		return
	}
	rendered := ansi.Render(i18n.FillScreen(lang, raw), previewVars(c.BBS.Name, c.BBS.Sysop))
	rendered = ansi.Layout(rendered, previewWidth)

	preformatted := ansi.IsPreformatted(rendered)
	var grid *ansi.Grid
	if preformatted {
		g := ansi.ParseGrid(rendered, previewWidth)
		grid = &g
	}
	writeJSON(w, http.StatusOK, welcomeScreenDTO{
		HTML:         ansi.ToHTML(rendered),
		Preformatted: preformatted,
		Grid:         grid,
	})
}
