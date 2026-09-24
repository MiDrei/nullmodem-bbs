package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/config"
)

func TestWelcomeScreenRendersPlaceholdersUnauthenticated(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Routes()

	// Deliberately no token -- this is the portal login page's own
	// pre-auth banner.
	rec := doJSON(t, h, http.MethodGet, "/api/bbs/welcome-screen", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got welcomeScreenDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(got.HTML, "Test BBS") {
		t.Fatalf("HTML = %q, want it to contain the rendered BBSNAME placeholder", got.HTML)
	}
	// The seeded welcome.ans carries real ANSI escape codes, so it must
	// come back as real preformatted art (rendered via AnsiArt, not
	// HTML/font-based spans) -- see welcomeScreenDTO's own doc comment.
	if !got.Preformatted || got.Grid == nil {
		t.Fatalf("Preformatted/Grid = %v/%v, want true/non-nil for real ANSI art", got.Preformatted, got.Grid)
	}
}

func TestWelcomeScreenReturns404WhenMissing(t *testing.T) {
	srv, _, configPath := newTestServer(t)
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := os.Remove(filepath.Join(c.BBS.ScreensDir, "welcome.ans")); err != nil {
		t.Fatalf("remove welcome.ans: %v", err)
	}

	h := srv.Routes()
	rec := doJSON(t, h, http.MethodGet, "/api/bbs/welcome-screen", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}
