package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestListGroupsCombinesAndDedupsMessageAndFileNetworks(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/groups", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status = %d, want 401", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/groups", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var initial []string
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(initial) != 0 {
		t.Fatalf("initial groups = %v, want none (fresh install has no grouped areas)", initial)
	}

	if _, err := srv.Messages.CreateArea("dev", "Dev", "", "fsxNet", 0, 0); err != nil {
		t.Fatalf("CreateArea (message): %v", err)
	}
	if _, err := srv.Files.CreateArea("games", "Games", "", "fsxNet", 0, 0); err != nil {
		t.Fatalf("CreateArea (file): %v", err)
	}
	if _, err := srv.Files.CreateArea("hobby", "Hobby", "", "HobbyNet", 0, 0); err != nil {
		t.Fatalf("CreateArea (file): %v", err)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/groups", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{"HobbyNet", "fsxNet"}
	if len(got) != len(want) {
		t.Fatalf("groups = %v, want %v (fsxNet deduped across message+file areas)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("groups = %v, want %v", got, want)
		}
	}
}
