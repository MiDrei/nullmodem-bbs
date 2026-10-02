package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestCallerChat(t *testing.T) {
	srv, users, _ := newTestServer(t)
	srv.Chat = chat.NewStore(srv.DB)
	users.Register("root", "supersecret", user.SLSysop)
	users.Register("alice", "password123", user.SLNewUser)
	srv.Chat.SaveRoom(chat.RoomInfo{Name: "sysops", Title: "Sysops", MinSL: 200})
	srv.Chat.SaveRoom(chat.RoomInfo{Name: "main", Title: "Teleconference", DiscordChannel: "123"})
	h := srv.Routes()
	token := loginAsBBSUser(t, h, "alice", "password123")

	rec := doJSON(t, h, http.MethodGet, "/api/bbs/chat/rooms", nil, token)
	var rooms []bbsChatRoomDTO
	json.Unmarshal(rec.Body.Bytes(), &rooms)
	if len(rooms) != 1 || rooms[0].Name != "main" || rooms[0].DiscordChannel != "" || len(rooms[0].Bridges) != 1 {
		t.Fatalf("rooms: %s", rec.Body)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/bbs/chat/rooms/sysops", map[string]string{"action": "enter"}, token); rec.Code != http.StatusNotFound {
		t.Errorf("entered a room above her level: %d", rec.Code)
	}
	doJSON(t, h, http.MethodPost, "/api/bbs/chat/rooms/main", map[string]string{"action": "enter"}, token)
	doJSON(t, h, http.MethodPost, "/api/bbs/chat/rooms/main", map[string]string{"action": "say", "text": "hi"}, token)
	rec = doJSON(t, h, http.MethodGet, "/api/bbs/chat/rooms/main?after=0", nil, token)
	var st chatRoomState
	json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.Lines) != 2 || st.Lines[1].Text != "hi" || st.Lines[1].Source != "web" || len(st.Present) != 1 {
		t.Fatalf("room: %s", rec.Body)
	}
}
