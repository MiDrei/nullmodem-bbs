package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestPortalMessageSearch(t *testing.T) {
	srv, users, _ := newTestServer(t)
	h := srv.Routes()
	users.Register("maik", "password123", user.SLNewUser)
	bob, _ := users.Register("bob", "password123", user.SLNewUser)
	general, _ := srv.Messages.AreaByTag("general")
	cp := func(s string) string { return string(ansi.EncodeCP437(s)) }
	srv.Messages.PostMessage(general.ID, bob.ID, "All", cp("Grüße aus Zürich"), cp("Viele Grüße an alle, die MRC Chat mögen. "+strings.Repeat("Füllt. ", 40)))
	srv.Messages.PostMessage(general.ID, bob.ID, "All", "Weather", "sunny")
	token := loginAsBBSUser(t, h, "bob", "password123")

	rec := doJSON(t, h, http.MethodGet, "/api/bbs/messages/search?q="+url.QueryEscape("grüße"), nil, token)
	var hits []searchHitDTO
	json.Unmarshal(rec.Body.Bytes(), &hits)
	if rec.Code != http.StatusOK || len(hits) != 1 {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	h0 := hits[0]
	if h0.Subject != "Grüße aus Zürich" || h0.AreaTag != "general" || !strings.Contains(h0.Snippet, "Viele Grüße") || len([]rune(h0.Snippet)) > 145 {
		t.Fatalf("hit %+v", h0)
	}
	if rec = doJSON(t, h, http.MethodGet, "/api/bbs/messages/search?q=x", nil, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("one-letter search: %d", rec.Code)
	}
}

func TestSnippetLeavesOutTearlineAndOrigin(t *testing.T) {
	body := "Hello there\n\n--- NullModem BBS\n * Origin: Maiks Place (21:3/194)\n\x01PATH: 3/100"
	if got := snippet(body, "zzz"); got != "Hello there" {
		t.Fatalf("snippet %q", got)
	}
}
