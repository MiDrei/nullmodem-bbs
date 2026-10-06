package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestDoorBulletinsAndTemplateDefaults(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	users.Register("alice", "password123", user.SLNewUser)
	h := srv.Routes()
	admin := loginAsSysop(t, h, "root", "supersecret")
	caller := loginAsBBSUser(t, h, "alice", "password123")

	dir := filepath.Join(t.TempDir(), "immortal-barons")
	os.MkdirAll(filepath.Join(dir, "data", "bull"), 0o755)
	c, _ := config.Load(configPath)
	c.Doors = append(c.Doors, config.DoorConfig{Name: "Immortal Barons", Dir: dir, Exe: filepath.Join(dir, "immortal-barons")})
	config.Save(configPath, c)

	// An old install: the template's bulletins are offered, and taking
	// them tells the game to write them.
	rec := doJSON(t, h, http.MethodPost, "/api/door-bulletins/Immortal%20Barons", nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body)
	}
	if cfg, _ := os.ReadFile(filepath.Join(dir, "data", "bbs.cfg")); len(cfg) == 0 {
		t.Fatal("bbs.cfg not written")
	}
	c, _ = config.Load(configPath)
	if d := c.Doors[len(c.Doors)-1]; len(d.Bulletins) != 3 || d.Daily == "" {
		t.Fatalf("door: %+v", d)
	}

	// Not written yet: nothing; then the scoreboard, public.
	if rec := doJSON(t, h, http.MethodGet, "/api/public/door-bulletins", nil, ""); rec.Body.String() != "[]\n" {
		t.Fatalf("before: %s", rec.Body)
	}
	os.WriteFile(filepath.Join(dir, "data", "bull", "scores.ans"), []byte("\x1b[1;36mScoreboard\x1b[0m\n1. maik  9999\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "data", "bull", "tdynews.ans"), []byte("Today: nothing\n"), 0o644)
	var pub, all []doorBulletinDTO
	json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/public/door-bulletins", nil, "").Body.Bytes(), &pub)
	json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/bbs/door-bulletins", nil, caller).Body.Bytes(), &all)
	if len(pub) != 1 || pub[0].Title != "Immortal Barons: scoreboard" || pub[0].Grid.Height < 2 {
		t.Fatalf("public: %+v", pub)
	}
	if len(all) != 2 {
		t.Fatalf("caller sees %d", len(all))
	}
}

// A door added from its template before the game was told to write its
// bulletins: running the maintenance now tells it, once.
func TestRunDailyEnablesTheTemplateBulletins(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	h := srv.Routes()
	admin := loginAsSysop(t, h, "root", "supersecret")

	dir := filepath.Join(t.TempDir(), "immortal-barons")
	os.MkdirAll(filepath.Join(dir, "data"), 0o755)
	c, _ := config.Load(configPath)
	c.Doors = append(c.Doors, config.DoorConfig{Name: "Immortal Barons", Dir: dir, Exe: filepath.Join(dir, "immortal-barons"),
		Template: "immortal-barons", Daily: "immortal-barons -maint -data data",
		Bulletins: []config.DoorBulletin{{Title: "Immortal Barons: scoreboard", File: "data/bull/scores.ans", Public: true}}})
	config.Save(configPath, c)

	for range 2 {
		if rec := doJSON(t, h, http.MethodPost, "/api/door-daily/Immortal%20Barons", nil, admin); rec.Code != http.StatusAccepted {
			t.Fatalf("run now: %d %s", rec.Code, rec.Body)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "data", "bbs.cfg"))
	if n := strings.Count(string(cfg), "BulletinDir"); n != 1 {
		t.Fatalf("bbs.cfg has BulletinDir %d times:\n%s", n, cfg)
	}
}
