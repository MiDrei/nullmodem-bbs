package web

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

// Deleting an approved account: not one's own, not while it's online;
// gone afterwards.
func TestDeleteApprovedUser(t *testing.T) {
	srv, users, _ := newTestServer(t)
	root, _ := users.Register("root", "supersecret", user.SLSysop)
	alice, _ := users.Register("alice", "password123", 50)
	bob, _ := users.Register("bob", "password123", 50)
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	if rec := doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/users/%d", root.ID), nil, token); rec.Code != http.StatusConflict {
		t.Errorf("own account: %d", rec.Code)
	}
	node, err := srv.Nodes.Join("127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	srv.Nodes.SetUsername(node, "bob")
	if rec := doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/users/%d", bob.ID), nil, token); rec.Code != http.StatusConflict {
		t.Errorf("online account: %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/users/%d", alice.ID), nil, token); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if _, err := users.ByID(alice.ID); err == nil {
		t.Error("account still there")
	}
}
