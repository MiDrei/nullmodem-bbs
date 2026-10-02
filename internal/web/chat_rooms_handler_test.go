package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestChatRoomSettingsAndDiscord(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	srv.Chat = chat.NewStore(srv.DB)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatal(err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodPut, "/api/chat/room-settings/tech", map[string]any{"title": "Technik", "discord_channel": "123456"}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/chat/room-settings/retro", map[string]any{"title": "Retro", "discord_channel": "123456"}, token); rec.Code != http.StatusBadRequest {
		t.Errorf("a channel bridged twice: %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/chat/room-settings/page-bob", map[string]any{"title": "x"}, token); rec.Code != http.StatusBadRequest {
		t.Errorf("a page room listed: %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodGet, "/api/chat/room-settings", nil, token)
	var rooms []chat.RoomInfo
	json.Unmarshal(rec.Body.Bytes(), &rooms)
	if len(rooms) != 2 || rooms[0].Name != "main" || rooms[1].DiscordChannel != "123456" {
		t.Fatalf("rooms %+v", rooms)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/chat/room-settings/main", nil, token); rec.Code != http.StatusBadRequest {
		t.Errorf("deleting main: %d", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/chat/room-settings/tech", nil, token); rec.Code != http.StatusNoContent {
		t.Errorf("deleting tech: %d", rec.Code)
	}

	// The bot: on needs a token; the token never comes back.
	if rec := doJSON(t, h, http.MethodPut, "/api/chat/discord", map[string]any{"enabled": true}, token); rec.Code != http.StatusBadRequest {
		t.Errorf("on without a token: %d", rec.Code)
	}
	secret := "MTE" + strings.Repeat("x", 60)
	rec = doJSON(t, h, http.MethodPut, "/api/chat/discord", map[string]any{"enabled": true, "token": "Bot " + secret}, token)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	c, _ := config.Load(configPath)
	if c.Discord.Token != secret || !c.Discord.Enabled {
		t.Fatalf("config %+v", c.Discord)
	}
	// Turning it off keeps the token.
	doJSON(t, h, http.MethodPut, "/api/chat/discord", map[string]any{"enabled": false}, token)
	rec = doJSON(t, h, http.MethodGet, "/api/chat/discord", nil, token)
	if !strings.Contains(rec.Body.String(), `"has_token":true`) || strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("get: %s", rec.Body)
	}
}
