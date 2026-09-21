package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
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
