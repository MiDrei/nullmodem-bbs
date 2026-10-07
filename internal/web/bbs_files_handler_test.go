package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestBBSFileAreasRespectMinSLDownload(t *testing.T) {
	srv, users, _ := newTestServer(t)
	// Bootstrap a sysop first so alice (registered second) isn't
	// auto-promoted by the first-user-becomes-sysop rule.
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := srv.Files.CreateArea("locked", "Locked", "", "", 200, 200); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, "/api/bbs/file-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var areas []bbsFileAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &areas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(areas) != 1 || areas[0].Tag != "general" {
		t.Fatalf("areas = %+v, want just the seeded general area", areas)
	}
}

func TestBBSUploadAndDownloadFileRoundTrip(t *testing.T) {
	srv, users, _ := newTestServer(t)
	// Bootstrap a sysop first so alice (registered second) isn't
	// auto-promoted by the first-user-becomes-sysop rule.
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	uploadLocked, err := srv.Files.CreateArea("uploadlocked", "Upload Locked", "", "", 0, 200)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// Upload rejected: min_sl_upload 200 > alice's SL.
	rec := multipartUpload(t, h, fmt.Sprintf("/api/bbs/file-areas/%d/files", uploadLocked.ID), "hello.txt", "hello world", token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("upload to upload-locked area status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	// Upload to the seeded general area works.
	areas, _ := srv.Files.AllAreas()
	var generalID int64
	for _, a := range areas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}
	rec = multipartUpload(t, h, fmt.Sprintf("/api/bbs/file-areas/%d/files", generalID), "hello.txt", "hello world", token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	var uploaded bbsFileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode uploaded: %v", err)
	}
	if uploaded.Filename != "hello.txt" || uploaded.UploadedBy != "alice" {
		t.Fatalf("uploaded = %+v, want filename=hello.txt uploaded_by=alice", uploaded)
	}

	// Download streams the exact bytes back and records the download.
	path := fmt.Sprintf("/api/bbs/files/%d/download", uploaded.ID)
	rec = doJSON(t, h, http.MethodGet, path, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("downloaded body = %q, want %q", rec.Body.String(), "hello world")
	}

	f, err := srv.Files.FileByID(uploaded.ID)
	if err != nil {
		t.Fatalf("FileByID: %v", err)
	}
	if f.DownloadCount != 1 {
		t.Fatalf("DownloadCount = %d, want 1", f.DownloadCount)
	}
}

// TestBBSGetFileReturnsDescriptionAndMarksRead locks in
// handleGetBBSFile: a file's full (here, multi-line -- the way a real
// TIC's Desc+Ldesc lines join, see internal/tic.File.Description)
// description must come back, and viewing it must mark it read --
// the only other way to do that is downloading, which isn't always
// wanted just to see what a file actually is.
func TestBBSGetFileReturnsDescriptionAndMarksRead(t *testing.T) {
	srv, users, _ := newTestServer(t)
	// Bootstrap a sysop first so alice (registered second) isn't
	// auto-promoted by the first-user-becomes-sysop rule.
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	areas, err := srv.Files.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	var generalID int64
	for _, a := range areas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}
	desc := "A short one-liner\nAn additional detail line\nAnother detail line"
	uploaded, err := srv.Files.UploadFile(generalID, alice.ID, "readme.txt", desc, strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d", uploaded.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got bbsFileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Description != desc {
		t.Fatalf("Description = %q, want the full multi-line description %q", got.Description, desc)
	}

	read, err := srv.Files.ReadFileIDs(alice.ID, generalID)
	if err != nil {
		t.Fatalf("ReadFileIDs: %v", err)
	}
	if !read[uploaded.ID] {
		t.Fatal("file not marked read after GET /api/bbs/files/{id}")
	}
}

func TestBBSGetFileRespectsMinSLDownload(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	sysop, err := users.Register("sysop2", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	locked, err := srv.Files.CreateArea("locked", "Locked", "", "", 200, 200)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	uploaded, err := srv.Files.UploadFile(locked.ID, sysop.ID, "secret.txt", "shh", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d", uploaded.ID), nil, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPreviewBBSFileRendersTextFiles(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	areas, err := srv.Files.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	var generalID int64
	for _, a := range areas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}
	uploaded, err := srv.Files.UploadFile(generalID, alice.ID, "readme.nfo", "", strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview", uploaded.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got filePreviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "text" {
		t.Fatalf("Kind = %q, want text", got.Kind)
	}
	if !strings.Contains(got.BodyHTML, "hello world") {
		t.Fatalf("BodyHTML = %q, want it to contain the file's text", got.BodyHTML)
	}
	if got.Truncated {
		t.Fatal("Truncated = true for a short file")
	}
}

func TestPreviewBBSFileListsZipEntries(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	areas, err := srv.Files.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	var generalID int64
	for _, a := range areas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zf, err := zw.Create("readme.txt")
	if err != nil {
		t.Fatalf("zip Create: %v", err)
	}
	if _, err := zf.Write([]byte("hi")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	uploaded, err := srv.Files.UploadFile(generalID, alice.ID, "bundle.zip", "", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview", uploaded.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got filePreviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "archive" {
		t.Fatalf("Kind = %q, want archive", got.Kind)
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "readme.txt" || got.Entries[0].SizeBytes != 2 {
		t.Fatalf("Entries = %+v, want the one readme.txt entry", got.Entries)
	}
}

func TestPreviewBBSFileReturnsNoneForUnrecognizedFiles(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	areas, err := srv.Files.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	var generalID int64
	for _, a := range areas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}
	uploaded, err := srv.Files.UploadFile(generalID, alice.ID, "game.exe", "", strings.NewReader("binary"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview", uploaded.ID), nil, token)
	var got filePreviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "none" {
		t.Fatalf("Kind = %q, want none", got.Kind)
	}
}

func TestPreviewBBSFileRespectsMinSLDownload(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	sysop, err := users.Register("sysop2", "password123", user.SLSysop)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	locked, err := srv.Files.CreateArea("locked", "Locked", "", "", 200, 200)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	uploaded, err := srv.Files.UploadFile(locked.ID, sysop.ID, "secret.txt", "shh", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview", uploaded.ID), nil, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("preview status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview-raw", uploaded.ID), nil, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("preview-raw status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPreviewRawBBSFileStreamsImageBytesWithoutCountingAsDownload(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	areas, err := srv.Files.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	var generalID int64
	for _, a := range areas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}
	uploaded, err := srv.Files.UploadFile(generalID, alice.ID, "shot.png", "", strings.NewReader("not really a png but bytes are bytes"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview-raw", uploaded.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("Content-Disposition = %q, want empty (inline, not a forced download)", rec.Header().Get("Content-Disposition"))
	}

	f, err := srv.Files.FileByID(uploaded.ID)
	if err != nil {
		t.Fatalf("FileByID: %v", err)
	}
	if f.DownloadCount != 0 {
		t.Fatalf("DownloadCount = %d, want 0 -- a preview isn't a download", f.DownloadCount)
	}
}

func TestPreviewBBSFileEntryRendersAFileInsideAZip(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	areas, err := srv.Files.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	var generalID int64
	for _, a := range areas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zf, err := zw.Create("readme.txt")
	if err != nil {
		t.Fatalf("zip Create: %v", err)
	}
	if _, err := zf.Write([]byte("hello from inside the zip")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	imgf, err := zw.Create("photo.png")
	if err != nil {
		t.Fatalf("zip Create: %v", err)
	}
	if _, err := imgf.Write([]byte("not really png bytes")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	uploaded, err := srv.Files.UploadFile(generalID, alice.ID, "bundle.zip", "", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview-entry?name=readme.txt", uploaded.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got filePreviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "text" || !strings.Contains(got.BodyHTML, "hello from inside the zip") {
		t.Fatalf("preview-entry readme.txt = %+v, want the zip entry's own text", got)
	}

	rec = doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview-entry-raw?name=photo.png", uploaded.ID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET raw status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if rec.Body.String() != "not really png bytes" {
		t.Fatalf("raw entry body = %q, want the exact zip entry bytes", rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/bbs/files/%d/preview-entry?name=nonexistent.txt", uploaded.ID), nil, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "none" {
		t.Fatalf("preview-entry for a name not in the zip = %+v, want kind none", got)
	}
}

func multipartUpload(t *testing.T, h http.Handler, path, filename, content, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
