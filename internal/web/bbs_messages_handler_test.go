package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func loginAsBBSUser(t *testing.T, h http.Handler, username, password string) string {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/api/bbs/auth/login", map[string]string{
		"username": username, "password": password,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("BBS login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode BBS login response: %v", err)
	}
	return resp.Token
}

func TestBBSLoginAcceptsAnySecurityLevelButRejectsBadCreds(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()

	// Below sysop level -- the sysop admin login would reject this.
	rec := doJSON(t, h, http.MethodPost, "/api/bbs/auth/login", map[string]string{
		"username": "alice", "password": "password123",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("BBS login for a non-sysop status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodPost, "/api/bbs/auth/login", map[string]string{
		"username": "alice", "password": "wrong",
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("BBS login with bad password status = %d, want 401", rec.Code)
	}
}

func TestBBSMessageAreasRespectMinSLRead(t *testing.T) {
	srv, users, _ := newTestServer(t)
	// Bootstrap a sysop first so alice (registered second) isn't
	// auto-promoted by the first-user-becomes-sysop rule.
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// "general" (seeded) has min_sl_read 0; add one alice can't read.
	if _, err := srv.Messages.CreateArea("locked", "Locked Area", "", "", 200, 200); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, "/api/bbs/message-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var areas []bbsMessageAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &areas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(areas) != 1 || areas[0].Tag != "general" {
		t.Fatalf("areas = %+v, want just the seeded general area (locked area's min_sl_read excludes alice)", areas)
	}
}

func TestBBSMessageAreasIncludeDescription(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := srv.Messages.CreateArea("chat", "Chat", "General chat for everyone", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, "/api/bbs/message-areas", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var areas []bbsMessageAreaDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &areas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var chat *bbsMessageAreaDTO
	for i := range areas {
		if areas[i].Tag == "chat" {
			chat = &areas[i]
		}
	}
	if chat == nil || chat.Description != "General chat for everyone" {
		t.Fatalf("areas = %+v, want the chat area's description included", areas)
	}
}

func TestBBSPostMessageRespectsMinSLWrite(t *testing.T) {
	srv, users, _ := newTestServer(t)
	// Bootstrap a sysop first so alice (registered second) isn't
	// auto-promoted by the first-user-becomes-sysop rule.
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := srv.Messages.CreateArea("readonly", "Read Only", "", "", 0, 200)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	// Can't post: min_sl_write 200 > alice's SL 10.
	path := fmt.Sprintf("/api/bbs/message-areas/%d/messages", area.ID)
	rec := doJSON(t, h, http.MethodPost, path, map[string]string{
		"subject": "Hi", "body": "hello",
	}, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST to a write-locked area status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	// Can post to the seeded "general" area (min_sl_write 0).
	generalAreas, _ := srv.Messages.AllAreas()
	var generalID int64
	for _, a := range generalAreas {
		if a.Tag == "general" {
			generalID = a.ID
		}
	}
	path = fmt.Sprintf("/api/bbs/message-areas/%d/messages", generalID)
	rec = doJSON(t, h, http.MethodPost, path, map[string]string{
		"subject": "Hi", "body": "hello there",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST to general status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	var created bbsMessageDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.FromName != "alice" || created.Subject != "Hi" || created.ToName != "All" {
		t.Fatalf("created = %+v, want from_name=alice subject=Hi to_name=All", created)
	}

	// Reading it back marks it read and renders body_html.
	getPath := fmt.Sprintf("/api/bbs/messages/%d", created.ID)
	rec = doJSON(t, h, http.MethodGet, getPath, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET message status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got bbsMessageDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.BodyHTML == "" {
		t.Fatal("body_html should not be empty")
	}
}

func TestBBSPostMessageRoundTripsNonASCIIText(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("bootstrap-sysop", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	areas, _ := srv.Messages.AllAreas()
	generalID := areas[0].ID
	path := fmt.Sprintf("/api/bbs/message-areas/%d/messages", generalID)
	const want = "Hallo von der neuen Web-Oberfläche! Grüße, äöüÄÖÜß"
	rec := doJSON(t, h, http.MethodPost, path, map[string]string{
		"subject": "Umlaut test", "body": want,
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created bbsMessageDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.Body != want {
		t.Fatalf("created.Body = %q, want %q (CP437 round-trip must preserve non-ASCII text)", created.Body, want)
	}

	getPath := fmt.Sprintf("/api/bbs/messages/%d", created.ID)
	rec = doJSON(t, h, http.MethodGet, getPath, nil, token)
	var got bbsMessageDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Body != want {
		t.Fatalf("re-fetched Body = %q, want %q", got.Body, want)
	}
}

func TestBBSListMessagesPaginates(t *testing.T) {
	srv, users, _ := newTestServer(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	areas, _ := srv.Messages.AllAreas()
	generalID := areas[0].ID
	for i := 0; i < 5; i++ {
		if _, err := srv.Messages.PostMessage(generalID, alice.ID, "All", fmt.Sprintf("Subject %d", i), "body"); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	path := fmt.Sprintf("/api/bbs/message-areas/%d/messages?limit=2&offset=1", generalID)
	rec := doJSON(t, h, http.MethodGet, path, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var page bbsMessagePageDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 5 || len(page.Messages) != 2 || page.Messages[0].Subject != "Subject 1" {
		t.Fatalf("page = %+v, want total=5 len=2 first subject=Subject 1", page)
	}
}

func TestBBSMarkAreaReadClearsTheAreasUnreadCount(t *testing.T) {
	srv, users, _ := newTestServer(t)
	// The first user becomes sysop; alice must be an ordinary one.
	users.Register("bootstrap-sysop", "password123", user.SLNewUser)
	alice, _ := users.Register("alice", "password123", user.SLNewUser)
	area, _ := srv.Messages.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	restricted, _ := srv.Messages.CreateArea("SYSOP", "Sysop", "", "", 200, 200)
	for i := 0; i < 3; i++ {
		srv.Messages.ReceiveEcho(area.ID, "Bot", fmt.Sprintf("Stats %d", i), "data", "", time.Now())
	}
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodPost, fmt.Sprintf("/api/bbs/message-areas/%d/mark-read", area.ID), nil, token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"marked":3`) {
		t.Fatalf("mark-read: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	stats, _ := srv.Messages.ListAreaStats(user.SLNewUser, alice.ID)
	for _, s := range stats {
		if s.Area.ID == area.ID && s.New != 0 {
			t.Fatalf("FSX_DAT still has %d new after mark-read", s.New)
		}
	}
	if rec := doJSON(t, h, http.MethodPost, fmt.Sprintf("/api/bbs/message-areas/%d/mark-read", restricted.ID), nil, token); rec.Code != http.StatusForbidden {
		t.Fatalf("mark-read on an area above the caller's SL: status = %d, want 403", rec.Code)
	}
}

func TestLastCallersEndpointAndHiddenDataArea(t *testing.T) {
	srv, users, _ := newTestServer(t)
	users.Register("bootstrap-sysop", "password123", user.SLNewUser)
	users.Register("alice", "password123", user.SLNewUser)
	area, _ := srv.Messages.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	srv.Messages.ReceiveEcho(area.ID, "ibbslastcall", "ibbslastcall-data",
		">>> BEGIN\n~D:C@?\n%96 )\\q:E qq$\n_h^b_^ae\n_fi_c2\nqF6?2 !2C<[ rp\n(:?5@HD\nI\\3:E]@C8\n>>> END\n", "21:4/107 1", time.Now())
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, "/api/bbs/last-callers", nil, token)
	var got []lastCallerDTO
	json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || len(got) != 1 || got[0].Alias != "Osiron" || got[0].BBS != "The X-Bit BBS" || got[0].Location != "Buena Park, CA" {
		t.Fatalf("last callers: status %d, %+v", rec.Code, got)
	}

	srv.Messages.SetAreaHidden(area.ID, true)
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/message-areas", nil, token)
	if strings.Contains(rec.Body.String(), "FSX_DAT") {
		t.Fatal("a hidden data area is listed for callers")
	}
	// Still read for the list, hidden or not.
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/last-callers", nil, token)
	if !strings.Contains(rec.Body.String(), "Osiron") {
		t.Fatal("hiding the data area emptied the last callers list")
	}
}
