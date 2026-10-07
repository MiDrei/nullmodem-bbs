package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/community"
	"github.com/midrei/nullmodem-bbs/internal/nodelist"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestPortalPollsAndBBSList(t *testing.T) {
	srv, users, _ := newTestServer(t)
	srv.Community = community.NewStore(users.DB())
	srv.Nodelist = nodelist.NewStore(users.DB())
	h := srv.Routes()
	users.Register("maik", "password123", user.SLNewUser) // sysop
	users.Register("alice", "password123", user.SLNewUser)
	users.Register("bob", "password123", user.SLNewUser)
	alice := loginAsBBSUser(t, h, "alice", "password123")
	bob := loginAsBBSUser(t, h, "bob", "password123")

	id, _ := srv.Community.CreatePoll("Best door?", []string{"LORD", "TradeWars"})
	p, _ := srv.Community.Poll(id, 0)
	rec := doJSON(t, h, http.MethodPost, "/api/bbs/polls/"+itoa(id)+"/vote", map[string]int64{"option_id": p.Options[1].ID}, alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("vote: %d %s", rec.Code, rec.Body.String())
	}
	var voted community.Poll
	json.Unmarshal(rec.Body.Bytes(), &voted)
	if voted.MyVote != p.Options[1].ID || voted.Total != 1 {
		t.Fatalf("after voting %+v", voted)
	}

	rec = doJSON(t, h, http.MethodPost, "/api/bbs/bbslist", map[string]string{"name": "Agency BBS", "address": "agency.bbs.nz:2323"}, alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	var e community.BBS
	json.Unmarshal(rec.Body.Bytes(), &e)
	if rec = doJSON(t, h, http.MethodDelete, "/api/bbs/bbslist/"+itoa(e.ID), nil, bob); rec.Code != http.StatusForbidden {
		t.Fatalf("someone else deleted it: %d", rec.Code)
	}
	if rec = doJSON(t, h, http.MethodPut, "/api/bbs/bbslist/"+itoa(e.ID), map[string]string{"name": "Agency", "address": "agency.bbs.nz:23"}, alice); rec.Code != http.StatusOK {
		t.Fatalf("own edit: %d", rec.Code)
	}
	if rec = doJSON(t, h, http.MethodDelete, "/api/bbs/bbslist/"+itoa(e.ID), nil, alice); rec.Code != http.StatusNoContent {
		t.Fatalf("own delete: %d", rec.Code)
	}
}
