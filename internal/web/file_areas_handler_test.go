package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestFileAreaCRUD(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/file-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var list []fileAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 || list[0].Tag != "general" {
		t.Fatalf("initial list = %+v, want just the seeded general area", list)
	}

	rec = doJSON(t, h, http.MethodPost, "/api/file-areas", fileAreaDTO{
		Tag: "doors", Name: "Door Games", MinSLDownload: 0, MinSLUpload: 100,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created fileAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	path := fmt.Sprintf("/api/file-areas/%d", created.ID)
	rec = doJSON(t, h, http.MethodPut, path, fileAreaDTO{
		Name: "Door Games Renamed", MinSLDownload: 5, MinSLUpload: 100, SortOrder: 2,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated fileAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated.Name != "Door Games Renamed" || updated.MinSLDownload != 5 {
		t.Fatalf("updated = %+v, want renamed fields", updated)
	}

	rec = doJSON(t, h, http.MethodDelete, path, nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func multipartUploadRequest(t *testing.T, url, filename, description, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.WriteField("description", description); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, url, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestUploadListAndDeleteFile(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	area, err := srv.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}

	uploadURL := fmt.Sprintf("/api/file-areas/%d/files", area.ID)
	req := multipartUploadRequest(t, uploadURL, "readme.txt", "a test file", "hello world")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var uploaded fileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode uploaded: %v", err)
	}
	if uploaded.Filename != "readme.txt" || uploaded.UploadedBy != "root" || uploaded.SizeBytes != int64(len("hello world")) {
		t.Fatalf("uploaded = %+v, want filename=readme.txt uploaded_by=root size=%d", uploaded, len("hello world"))
	}

	// Unauthenticated upload is rejected.
	req2 := multipartUploadRequest(t, uploadURL, "other.txt", "", "x")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated upload status = %d, want 401", rec2.Code)
	}

	// List files in the area.
	rec = doJSON(t, h, http.MethodGet, uploadURL, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list files status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var files []fileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v, want 1", files)
	}

	// Delete it.
	delPath := fmt.Sprintf("/api/files/%d", uploaded.ID)
	rec = doJSON(t, h, http.MethodDelete, delPath, nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodDelete, delPath, nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete again status = %d, want 404", rec.Code)
	}
}

func TestUploadRejectsDuplicateFilename(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	area, err := srv.Files.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	uploadURL := fmt.Sprintf("/api/file-areas/%d/files", area.ID)

	for i, wantCode := range []int{http.StatusCreated, http.StatusConflict} {
		req := multipartUploadRequest(t, uploadURL, "dup.txt", "", fmt.Sprintf("content %d", i))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != wantCode {
			t.Fatalf("upload #%d status = %d, want %d, body=%s", i, rec.Code, wantCode, rec.Body.String())
		}
	}
}
