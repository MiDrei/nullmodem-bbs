package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestPendingAreasListsOnlyPendingOnesAndApproveMakesThemVisible(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	// Unauthenticated request rejected.
	rec := doJSON(t, h, http.MethodGet, "/api/pending-areas", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status = %d, want 401", rec.Code)
	}

	// Nothing pending yet -- only the seeded, already-approved areas exist.
	rec = doJSON(t, h, http.MethodGet, "/api/pending-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got pendingAreasDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.MessageAreas) != 0 || len(got.FileAreas) != 0 {
		t.Fatalf("initial pending areas = %+v, want none", got)
	}

	// Simulate what internal/tosser's echomail toss does: auto-create
	// a pending message area.
	area, _, err := srv.Messages.EnsureArea("FSXNET_GENERAL", "FSXNET_GENERAL", "fsxNet")
	if err != nil {
		t.Fatalf("EnsureArea: %v", err)
	}

	// It must not show up in the normal Message Areas listing yet.
	rec = doJSON(t, h, http.MethodGet, "/api/message-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/message-areas status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var normalList []messageAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &normalList); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, a := range normalList {
		if a.Tag == "FSXNET_GENERAL" {
			t.Fatalf("normal message-areas listing included the pending area: %+v", normalList)
		}
	}

	// But it does show up in Pending Areas.
	rec = doJSON(t, h, http.MethodGet, "/api/pending-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.MessageAreas) != 1 || got.MessageAreas[0].Tag != "FSXNET_GENERAL" || !got.MessageAreas[0].Pending {
		t.Fatalf("pending message areas = %+v, want just FSXNET_GENERAL, pending", got.MessageAreas)
	}

	// Approve it.
	rec = doJSON(t, h, http.MethodPost, "/api/pending-areas/message-areas/"+strconv.FormatInt(area.ID, 10)+"/approve", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var approved messageAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &approved); err != nil {
		t.Fatalf("decode approved: %v", err)
	}
	if approved.Pending {
		t.Fatal("approved area still reports Pending = true")
	}

	// Now it's gone from Pending Areas and present in the normal list.
	rec = doJSON(t, h, http.MethodGet, "/api/pending-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.MessageAreas) != 0 {
		t.Fatalf("pending message areas after approval = %+v, want none", got.MessageAreas)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/message-areas", nil, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &normalList); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, a := range normalList {
		if a.Tag == "FSXNET_GENERAL" {
			found = true
		}
	}
	if !found {
		t.Fatal("normal message-areas listing did not include the approved area")
	}
}

func TestApprovePendingFileAreaMirrorsMessageAreas(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	area, _, err := srv.Files.EnsureArea("SOME_FILE_ECHO", "SOME_FILE_ECHO", "")
	if err != nil {
		t.Fatalf("EnsureArea: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/pending-areas", nil, token)
	var got pendingAreasDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.FileAreas) != 1 || got.FileAreas[0].Tag != "SOME_FILE_ECHO" {
		t.Fatalf("pending file areas = %+v, want just SOME_FILE_ECHO", got.FileAreas)
	}

	rec = doJSON(t, h, http.MethodPost, "/api/pending-areas/file-areas/"+strconv.FormatInt(area.ID, 10)+"/approve", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var approved fileAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &approved); err != nil {
		t.Fatalf("decode approved: %v", err)
	}
	if approved.Pending {
		t.Fatal("approved file area still reports Pending = true")
	}
}
