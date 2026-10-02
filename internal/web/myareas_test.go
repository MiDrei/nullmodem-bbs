package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestMyAreasViaPortal(t *testing.T) {
	srv, users, _ := newTestServer(t)
	users.Register("bootstrap", "password123", user.SLSysop)
	users.Register("alice", "password123", user.SLNewUser)
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")
	general, _ := srv.Messages.AreaByTag("general")
	other, _ := srv.Messages.CreateArea("other", "Other", "", "", 0, 0)

	areas := func() map[int64]bool {
		rec := doJSON(t, h, http.MethodGet, "/api/bbs/message-areas", nil, token)
		var list []bbsMessageAreaDTO
		json.Unmarshal(rec.Body.Bytes(), &list)
		out := map[int64]bool{}
		for _, a := range list {
			out[a.ID] = a.Mine
		}
		return out
	}
	if got := areas(); !got[general.ID] || !got[other.ID] {
		t.Fatalf("everything is mine at first: %v", got)
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/bbs/message-areas/"+itoa(general.ID)+"/mine", map[string]bool{"mine": false}, token); rec.Code != http.StatusNoContent {
		t.Fatalf("toggle: %d %s", rec.Code, rec.Body)
	}
	if got := areas(); got[general.ID] || !got[other.ID] {
		t.Fatalf("after taking general out: %v", got)
	}
	// Picking none in the QWK selection really is none (it used to be all).
	if rec := doJSON(t, h, http.MethodPut, "/api/bbs/qwk/areas", map[string][]int64{"area_ids": {}}, token); rec.Code != http.StatusNoContent {
		t.Fatalf("qwk areas: %d %s", rec.Code, rec.Body)
	}
	if got := areas(); got[general.ID] || got[other.ID] {
		t.Fatalf("after picking none: %v", got)
	}
}
