package web

import (
	"net/http"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestAdminClearsARoomAndQuietsSysops(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	if srv.Chat == nil {
		srv.Chat = chat.NewStore(srv.DB)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	srv.Chat.Post(chat.Main, "bob", "node 1", chat.Say, "hi")
	srv.Chat.Post(chat.Main, "bob", "node 1", chat.Say, "anyone?")
	rec := doJSON(t, h, http.MethodDelete, "/api/chat/rooms/main/lines", nil, token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"deleted":2`) {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body)
	}
	if lines, _ := srv.Chat.Lines(chat.Main, 0, 10); len(lines) != 0 {
		t.Fatalf("lines left: %+v", lines)
	}

	if rec := doJSON(t, h, http.MethodGet, "/api/chat/settings", nil, token); !strings.Contains(rec.Body.String(), `"announce_sysops":false`) {
		t.Fatalf("settings: %s", rec.Body)
	}
	doJSON(t, h, http.MethodPut, "/api/chat/settings", map[string]any{"announce_sysops": true}, token)
	if c, _ := config.Load(configPath); !c.BBS.ChatAnnounceSysops {
		t.Fatal("announce_sysops not saved")
	}
}
