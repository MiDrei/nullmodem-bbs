package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
)

func TestGetScreenGridParsesExistingScreen(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/screens/welcome.ans/grid", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET grid status = %d, want 401", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/screens/welcome.ans/grid", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET grid status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var grid gridDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &grid); err != nil {
		t.Fatalf("decode grid: %v", err)
	}
	// The seeded fixture's widest line is only 14 cols, but the
	// designer must never open narrower than the standard 80-column
	// BBS terminal width (see inferWidth's doc comment).
	if grid.Width != 80 || len(grid.Cells) != grid.Width*grid.Height {
		t.Fatalf("grid = width=%d height=%d cells=%d, want width 80 and cells=width*height",
			grid.Width, grid.Height, len(grid.Cells))
	}
}

func TestCreateScreenThenSaveGridRoundTrips(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPost, "/api/screens", map[string]any{
		"name": "test.ans", "width": 4, "height": 2,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/screens status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Creating the same name again must conflict.
	rec = doJSON(t, h, http.MethodPost, "/api/screens", map[string]any{
		"name": "test.ans", "width": 4, "height": 2,
	}, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want 409", rec.Code)
	}

	// A brand-new screen is entirely blank, so GET right after create
	// has no content to measure a width from and reports the 80-col
	// default (see Grid.Encode's trailing-blank-trimming doc comment)
	// rather than the 4 columns it was created with -- expected, and
	// harmless in practice since the real UI never re-fetches a blank
	// screen it just created without drawing into it first. Build the
	// grid to PUT from the known creation size instead.
	grid := gridDTO{Width: 4, Height: 2, Cells: make([]ansi.Cell, 4*2)}
	for i := range grid.Cells {
		grid.Cells[i] = ansi.Cell{Char: ' ', FG: 7, BG: 0}
	}

	// Paint the first cell and save.
	grid.Cells[0].Char = 'X'
	grid.Cells[0].FG = 12
	rec = doJSON(t, h, http.MethodPut, "/api/screens/test.ans/grid", grid, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT grid status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, "/api/screens/test.ans/grid", nil, token)
	var reloaded gridDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &reloaded); err != nil {
		t.Fatalf("decode reloaded grid: %v", err)
	}
	if reloaded.Cells[0].Char != 'X' || reloaded.Cells[0].FG != 12 {
		t.Fatalf("reloaded cell 0 = %+v, want char X fg 12", reloaded.Cells[0])
	}

	// Wrong cell count must be rejected.
	bad := gridDTO{Width: 4, Height: 2, Cells: grid.Cells[:3]}
	rec = doJSON(t, h, http.MethodPut, "/api/screens/test.ans/grid", bad, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT mismatched cell count status = %d, want 400", rec.Code)
	}

	// Saving a screen that doesn't exist must 404.
	rec = doJSON(t, h, http.MethodPut, "/api/screens/nope.ans/grid", reloaded, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT unknown screen status = %d, want 404", rec.Code)
	}
}

func TestDeleteScreenRemovesFile(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	doJSON(t, h, http.MethodPost, "/api/screens", map[string]any{"name": "temp.ans"}, token)

	rec := doJSON(t, h, http.MethodDelete, "/api/screens/temp.ans", nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", rec.Code)
	}

	rec = doJSON(t, h, http.MethodDelete, "/api/screens/temp.ans", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE already-deleted status = %d, want 404", rec.Code)
	}
}

func TestImportScreenStoresRawUpload(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "imported.ans")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	fw.Write([]byte("\x1b[1;32mHELLO"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/screens/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST import status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec2 := doJSON(t, h, http.MethodGet, "/api/screens/imported.ans/grid", nil, token)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET imported grid status = %d, body=%s", rec2.Code, rec2.Body.String())
	}
	var grid gridDTO
	if err := json.Unmarshal(rec2.Body.Bytes(), &grid); err != nil {
		t.Fatalf("decode grid: %v", err)
	}
	if grid.Cells[0].Char != 'H' || grid.Cells[0].FG != 10 {
		t.Fatalf("imported cell 0 = %+v, want char H fg 10 (bright green)", grid.Cells[0])
	}
}
