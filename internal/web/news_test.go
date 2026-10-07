package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/community"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestNewsAdminAndPublic(t *testing.T) {
	srv, users, _ := newTestServer(t)
	srv.Community = community.NewStore(users.DB())
	users.Register("root", "supersecret", user.SLSysop)
	h := srv.Routes()
	admin := loginAsSysop(t, h, "root", "supersecret")

	if rec := doJSON(t, h, http.MethodPost, "/api/news", map[string]string{"title_en": "Only a title"}, admin); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty news: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, h, http.MethodPost, "/api/news", map[string]string{"title_en": "Doors are back", "text_en": "Play.", "title_de": "Doors sind zurück", "text_de": "Spielt.", "expires_at": "2099-12-31"}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var list []newsDTO
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Author != "root" || list[0].ExpiresAt != "2099-12-31" {
		t.Fatalf("list: %+v", list)
	}
	id := list[0].ID

	// The front page: in the visitor's language.
	req := httptest.NewRequest(http.MethodGet, "/api/public/news", nil)
	req.Header.Set("X-Lang", "de-du")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	var pub []publicNewsDTO
	json.Unmarshal(out.Body.Bytes(), &pub)
	if len(pub) != 1 || pub[0].Title != "Doors sind zurück" {
		t.Fatalf("public: %s", out.Body)
	}

	if rec := doJSON(t, h, http.MethodPut, "/api/news/"+itoa(id), map[string]string{"title_en": "Doors!", "text_en": "Play."}, admin); rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/news/"+itoa(id), nil, admin); rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/news", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without login: %d", rec.Code)
	}
}
