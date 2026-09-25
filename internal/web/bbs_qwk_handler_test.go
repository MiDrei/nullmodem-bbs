package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"git.maik.ch/nullmodem/kit/qwk"
	"git.maik.ch/nullmodem/bbs/internal/qwkdoor"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestListBBSQWKAreasReflectsSelection(t *testing.T) {
	srv, users, _ := newTestServer(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	other, err := srv.Messages.CreateArea("other", "Other Area", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// Unconfigured: every readable area shows Selected=true.
	rec := doJSON(t, h, http.MethodGet, "/api/bbs/qwk/areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var areas []qwkAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &areas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(areas) != 2 {
		t.Fatalf("areas = %+v, want 2 (general + other)", areas)
	}
	for _, a := range areas {
		if !a.Selected {
			t.Fatalf("area %+v Selected = false, want true before any selection is saved", a)
		}
	}

	// Configure a selection excluding "other".
	if err := srv.Messages.SetQWKSelectedAreas(alice.ID, []int64{areas[0].ID}); err != nil {
		t.Fatalf("SetQWKSelectedAreas: %v", err)
	}
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/qwk/areas", nil, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &areas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, a := range areas {
		if a.ID == other.ID && a.Selected {
			t.Fatalf("area %+v should be unselected", a)
		}
	}
}

func TestSetBBSQWKAreasIgnoresUnreadableAreasAndClearsWhenAllSelected(t *testing.T) {
	srv, users, _ := newTestServer(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	locked, err := srv.Messages.CreateArea("locked", "Locked", "", "", 200, 200)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	general, err := srv.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// Selecting an area alice can't read is silently dropped, and
	// since that leaves every *readable* area selected, storage is
	// cleared back to "no explicit selection".
	rec := doJSON(t, h, http.MethodPut, "/api/bbs/qwk/areas", map[string]any{
		"area_ids": []int64{general.ID, locked.ID},
	}, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	selected, err := srv.Messages.QWKSelectedAreaIDs(alice.ID)
	if err != nil {
		t.Fatalf("QWKSelectedAreaIDs: %v", err)
	}
	if len(selected) != 0 {
		t.Fatalf("selected = %v, want empty (locked area dropped, general alone == 'all')", selected)
	}
}

func TestDownloadBBSQWKReturnsPacketOrNoContent(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// No new mail yet.
	rec := doJSON(t, h, http.MethodGet, "/api/bbs/qwk/download", nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("download with no new mail status = %d, want 204", rec.Code)
	}

	general, err := srv.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	// Posted by bob, not alice -- PostMessage now marks a poster's own
	// message read for themselves immediately (see its own doc
	// comment), so alice needs someone else's post to have new mail.
	if _, err := srv.Messages.PostMessage(general.ID, bob.ID, "All", "Hello", "a test message"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/bbs/qwk/download", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, body=%s", rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	found := false
	for _, zf := range zr.File {
		if zf.Name == "MESSAGES.DAT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("packet zip entries = %v, want MESSAGES.DAT", zr.File)
	}

	// The download must have marked the message read -- a second
	// download has nothing new to include.
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/qwk/download", nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second download status = %d, want 204 (already delivered)", rec.Code)
	}
}

func TestUploadBBSQWKReplyRoutesRepliesAndReturnsCounts(t *testing.T) {
	srv, users, _ := newTestServer(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := srv.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	bbsID := qwkdoor.BBSID("Test BBS")
	var repBuf bytes.Buffer
	zw := zip.NewWriter(&repBuf)
	mw, err := zw.Create(bbsID + ".MSG")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	if err := qwk.WriteMessagesDAT(mw, []qwk.PackedMessage{{
		Header: qwk.MessageHeader{
			Number:     int(general.ID), // REP repurposes this as the conference number
			To:         "All",
			Subject:    "Reply subject",
			Blocks:     1,
			Conference: int(general.ID),
		},
		Text: "reply body",
	}}); err != nil {
		t.Fatalf("WriteMessagesDAT: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	var body bytes.Buffer
	mpw := multipart.NewWriter(&body)
	part, err := mpw.CreateFormFile("file", "reply.rep")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(repBuf.Bytes()); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mpw.Close(); err != nil {
		t.Fatalf("mpw.Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/bbs/qwk/upload", &body)
	req.Header.Set("Content-Type", mpw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var resp struct{ Posted, Sent, Skipped int }
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Posted != 1 || resp.Sent != 0 || resp.Skipped != 0 {
		t.Fatalf("counts = %+v, want {Posted:1 Sent:0 Skipped:0}", resp)
	}

	msgs, err := srv.Messages.ListMessages(general.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	found := false
	for _, m := range msgs {
		if m.Subject == "Reply subject" && m.FromUserID.Valid && m.FromUserID.Int64 == alice.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("posted messages = %+v, want the uploaded reply", msgs)
	}
}

// failingWriter is a ResponseWriter whose connection drops after the
// headers: every body write fails.
type failingWriter struct {
	*httptest.ResponseRecorder
}

func (f failingWriter) Write([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestDownloadBBSQWKLeavesMessagesUnreadWhenNotFullyDelivered(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := srv.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if _, err := srv.Messages.PostMessage(general.ID, bob.ID, "All", "Hello", "a test message"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// A HEAD request delivers nothing.
	rec := doJSON(t, h, http.MethodHead, "/api/bbs/qwk/download", nil, token)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD status = %d, body len = %d, want 200 with no body", rec.Code, rec.Body.Len())
	}

	// The connection drops mid-transfer.
	req := httptest.NewRequest(http.MethodGet, "/api/bbs/qwk/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(failingWriter{httptest.NewRecorder()}, req)

	// Neither may have marked the message read.
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/qwk/download", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("download after failed attempts status = %d, want 200 (message still unread)", rec.Code)
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(rec.Body.Len()); got != want {
		t.Fatalf("Content-Length = %s, body is %s bytes", got, want)
	}
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/qwk/download", nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("download after a complete one status = %d, want 204", rec.Code)
	}
}
