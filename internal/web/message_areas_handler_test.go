package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestMessageAreaCRUD(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	// Unauthenticated list is rejected.
	rec := doJSON(t, h, http.MethodGet, "/api/message-areas", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status = %d, want 401", rec.Code)
	}

	// Initially just the seeded "general" area.
	rec = doJSON(t, h, http.MethodGet, "/api/message-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var list []messageAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 || list[0].Tag != "general" {
		t.Fatalf("initial list = %+v, want just the seeded general area", list)
	}

	// Create.
	rec = doJSON(t, h, http.MethodPost, "/api/message-areas", messageAreaDTO{
		Tag: "dev", Name: "Dev Talk", Description: "for devs", Network: "fsxNet", MinSLRead: 10, MinSLWrite: 50,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created messageAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.Tag != "dev" || created.MinSLWrite != 50 || created.Network != "fsxNet" {
		t.Fatalf("created = %+v, want tag=dev min_sl_write=50 network=fsxNet", created)
	}

	// Duplicate tag rejected.
	rec = doJSON(t, h, http.MethodPost, "/api/message-areas", messageAreaDTO{
		Tag: "dev", Name: "Dupe",
	}, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate tag POST status = %d, want 409", rec.Code)
	}

	// Invalid tag characters rejected.
	rec = doJSON(t, h, http.MethodPost, "/api/message-areas", messageAreaDTO{
		Tag: "bad tag!", Name: "Bad",
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid tag POST status = %d, want 400", rec.Code)
	}

	// Update.
	path := fmt.Sprintf("/api/message-areas/%d", created.ID)
	rec = doJSON(t, h, http.MethodPut, path, messageAreaDTO{
		Name: "Dev Talk Renamed", Description: "updated", Network: "FidoNet", MinSLRead: 20, MinSLWrite: 60, SortOrder: 3,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated messageAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated.Name != "Dev Talk Renamed" || updated.MinSLRead != 20 || updated.Tag != "dev" || updated.Network != "FidoNet" {
		t.Fatalf("updated = %+v, want renamed fields with tag unchanged and network=FidoNet", updated)
	}

	// Delete.
	rec = doJSON(t, h, http.MethodDelete, path, nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, "/api/message-areas", nil, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list after delete = %+v, want just the seeded general area", list)
	}
}
