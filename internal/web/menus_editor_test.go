package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/menu"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestMenuEditor(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")
	c, _ := config.Load(configPath)
	menu.Save(c.BBS.MenusDir, &menu.Menu{Name: "sysop", Title: "Sysop", Items: []menu.Item{{Key: "Q", Action: "back"}}})

	// This version's stock main menu has a command the board's lacks.
	defaults := t.TempDir()
	menu.Save(defaults, &menu.Menu{Name: "main", Title: "Main", Items: []menu.Item{
		{Key: "W", Label: "Who's online", Action: "builtin:who"}, {Key: "V", Label: "Voting booth", Action: "builtin:polls"},
	}})
	srv.MenuDefaultsDir = defaults

	rec := doJSON(t, h, http.MethodGet, "/api/menus/main", nil, token)
	var got menuEditDTO
	json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || len(got.MissingDefaults) != 1 || got.MissingDefaults[0].Action != "builtin:polls" {
		t.Fatalf("get main: %d %s", rec.Code, rec.Body)
	}

	// A new menu, and main leading there.
	games := menuDTO{Title: "Games", Items: []menuItemDTO{{Key: "d", Label: "Doors", Action: "builtin:doors"}, {Key: "Q", Label: "Back", Action: "back"}}}
	if rec := doJSON(t, h, http.MethodPut, "/api/menus/games", games, token); rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	main := got.Menu
	main.Items = append(main.Items, menuItemDTO{Key: "G", Label: "Games", Action: "goto:games"})
	if rec := doJSON(t, h, http.MethodPut, "/api/menus/main", main, token); rec.Code != http.StatusOK {
		t.Fatalf("save main: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, h, http.MethodDelete, "/api/menus/games", nil, token); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "main") {
		t.Errorf("deleting a menu still led to: %d %s", rec.Code, rec.Body)
	}
	bad := main
	bad.Items = append(append([]menuItemDTO{}, main.Items...), menuItemDTO{Key: "g", Action: "back"})
	if rec := doJSON(t, h, http.MethodPut, "/api/menus/main", bad, token); rec.Code != http.StatusBadRequest {
		t.Errorf("a key twice: %d", rec.Code)
	}
	set, _ := menu.LoadDir(c.BBS.MenusDir)
	if g, ok := set.Get("games"); !ok || g.Items[0].Key != "D" {
		t.Fatalf("games on disk: %+v", g)
	}

	// The preview: a screen that forgets an item says so.
	os.WriteFile(filepath.Join(c.BBS.ScreensDir, "games.ans"), []byte("\x1b[1;36mGAMES\x1b[0m\r\n [D] Doors\r\n"), 0o644)
	games.Name, games.Screen = "games", "games.ans"
	rec = doJSON(t, h, http.MethodPost, "/api/menu-preview", map[string]any{"menu": games, "sl": 10}, token)
	var pv menuPreviewDTO
	json.Unmarshal(rec.Body.Bytes(), &pv)
	if rec.Code != http.StatusOK || !pv.HasScreen || len(pv.NotShown) != 1 || pv.NotShown[0].Key != "Q" || pv.Grid.Height == 0 {
		t.Fatalf("preview: %d %+v", rec.Code, pv.NotShown)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/menu-actions", nil, token)
	if !strings.Contains(rec.Body.String(), `"newscan"`) || !strings.Contains(rec.Body.String(), `"games"`) {
		t.Errorf("actions: %s", rec.Body)
	}
}
