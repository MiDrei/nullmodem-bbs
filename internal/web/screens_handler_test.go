package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestListScreensReturnsSeededScreen(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/screens", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/screens status = %d, want 401", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/screens", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/screens status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var screens []screenSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &screens); err != nil {
		t.Fatalf("decode screens: %v", err)
	}
	if len(screens) != 1 || screens[0].Name != "welcome.ans" {
		t.Fatalf("screens = %+v, want one seeded welcome.ans", screens)
	}
}

func TestPreviewScreenRendersPlaceholdersAndColors(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/screens/welcome.ans", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/screens/welcome.ans status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var preview screenPreviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !strings.Contains(preview.HTML, "Test BBS") {
		t.Fatalf("preview HTML = %q, want {BBSNAME} rendered as 'Test BBS'", preview.HTML)
	}
	if !strings.Contains(preview.HTML, "color:#55FFFF") {
		t.Fatalf("preview HTML = %q, want bright cyan span for the SGR 1;36 run", preview.HTML)
	}
	if !strings.Contains(preview.HTML, "<br>") {
		t.Fatalf("preview HTML = %q, want CRLF converted to <br>", preview.HTML)
	}

	// Path traversal must resolve to filepath.Base, not escape ScreensDir.
	rec = doJSON(t, h, http.MethodGet, "/api/screens/..%2f..%2fetc%2fpasswd", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET traversal attempt status = %d, want 404", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/screens/doesnotexist.ans", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown screen status = %d, want 404", rec.Code)
	}
}
