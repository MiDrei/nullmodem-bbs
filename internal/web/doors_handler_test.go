package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/doors"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func doorsTestSetup(t *testing.T) (http.Handler, string, string, string) {
	t.Helper()
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	doorsDir := t.TempDir()
	c.BBS.DoorsDir = doorsDir
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	h := srv.Routes()
	return h, loginAsSysop(t, h, "root", "supersecret"), configPath, doorsDir
}

func TestPutDoorsValidatesAndSaves(t *testing.T) {
	h, token, configPath, _ := doorsTestSetup(t)

	bad := map[string]any{"doors": []doorDTO{{Name: "X", Kind: "dosbox", DOSBoxDir: "d", DOSBoxLaunchCmd: "X.EXE", DropFile: "chain.txt"}}}
	if rec := doJSON(t, h, http.MethodPut, "/api/doors", bad, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown drop file: status = %d, want 400", rec.Code)
	}
	escape := map[string]any{"doors": []doorDTO{{Name: "X", Kind: "dosbox", DOSBoxDir: "d", DOSBoxLaunchCmd: "X.EXE", LockFiles: []string{"../x"}}}}
	if rec := doJSON(t, h, http.MethodPut, "/api/doors", escape, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("escaping lock file: status = %d, want 400", rec.Code)
	}
	dup := map[string]any{"doors": []doorDTO{
		{Name: "LORD", Kind: "dosbox", DOSBoxDir: "a", DOSBoxLaunchCmd: "A"},
		{Name: "lord", Kind: "dosbox", DOSBoxDir: "b", DOSBoxLaunchCmd: "B"},
	}}
	if rec := doJSON(t, h, http.MethodPut, "/api/doors", dup, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate names: status = %d, want 400", rec.Code)
	}

	good := map[string]any{"doors": []doorDTO{{
		Name: "OO2", Kind: "dosbox", MinSL: 20, DOSBoxDir: "data/doors/oo2",
		DOSBoxLaunchCmd: "OOINFO.EXE 2 {dropfile_dir} {node}\nOOII.EXE", DropFile: "door.sys",
		LockFiles: []string{"OONODE.DAT", " "},
	}}}
	rec := doJSON(t, h, http.MethodPut, "/api/doors", good, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	c, _ := config.Load(configPath)
	if len(c.Doors) != 1 || c.Doors[0].MinSL != 20 || len(c.Doors[0].LockFiles) != 1 || c.Doors[0].DropFile != "door.sys" {
		t.Fatalf("saved doors = %+v", c.Doors)
	}
}

func TestAddDoorFromTemplateWithoutDownloadCreatesDirAndEntry(t *testing.T) {
	h, token, configPath, doorsDir := doorsTestSetup(t)

	rec := doJSON(t, h, http.MethodPost, "/api/doors/templates/lord", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if fi, err := os.Stat(filepath.Join(doorsDir, "lord")); err != nil || !fi.IsDir() {
		t.Fatalf("door dir not created: %v", err)
	}
	c, _ := config.Load(configPath)
	if len(c.Doors) != 1 {
		t.Fatalf("doors = %+v", c.Doors)
	}
	d := c.Doors[0]
	if d.Kind != "dosbox" || d.Template != "lord" || d.DropFile != "dorinfo" || !d.DropFileInDoorDir || d.DOSBoxDir != filepath.Join(doorsDir, "lord") {
		t.Fatalf("door = %+v", d)
	}

	if rec := doJSON(t, h, http.MethodPost, "/api/doors/templates/lord", nil, token); rec.Code != http.StatusConflict {
		t.Fatalf("second add: status = %d, want 409", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/doors/templates/nope", nil, token); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown template: status = %d, want 404", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/doors/templates", nil, token)
	var tmpls []doorTemplateDTO
	json.Unmarshal(rec.Body.Bytes(), &tmpls)
	for _, tm := range tmpls {
		if tm.ID == "lord" && !tm.Configured {
			t.Fatal("lord template not reported as configured")
		}
		if tm.ID == "judge-dredd" && (!tm.Downloadable || tm.Configured) {
			t.Fatalf("judge-dredd state = %+v", tm)
		}
	}
}

func TestDoorsRequireAuth(t *testing.T) {
	h, _, _, _ := doorsTestSetup(t)
	if rec := doJSON(t, h, http.MethodGet, "/api/doors", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestMRCDoorSettingsAndBridgeRestart: adding MRC Chat over a door
// directory that's already there writes the given mrc.cfg and sets up
// umrc-bridge as the door's background program; saving the settings
// later asks that program to restart, which the restart API allows.
func TestMRCDoorSettingsAndBridgeRestart(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	c, _ := config.Load(configPath)
	c.BBS.DoorsDir = t.TempDir()
	config.Save(configPath, c)
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	if rec := doJSON(t, h, http.MethodGet, "/api/doors/mrc", nil, token); rec.Code != http.StatusNotFound {
		t.Fatalf("GET without the door: status = %d, want 404", rec.Code)
	}

	dir := filepath.Join(c.BBS.DoorsDir, "umrc")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "umrc-client"), []byte("x"), 0o755)
	mrc := doors.DefaultMRCConfig("Test BBS", "Sysop")
	mrc.Website = "https://bbs.example"
	rec := doJSON(t, h, http.MethodPost, "/api/doors/templates/umrc", map[string]any{"mrc": mrc}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("add: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got, err := doors.ReadMRCConfig(dir); err != nil || got != mrc {
		t.Fatalf("mrc.cfg = %+v, %v", got, err)
	}
	c, _ = config.Load(configPath)
	if len(c.Doors) != 1 || strings.Join(c.Doors[0].Program, " ") != "umrc-bridge" || c.Doors[0].Template != "umrc" {
		t.Fatalf("door = %+v", c.Doors)
	}

	name := doors.Program{Door: c.Doors[0].Name}.ServiceName()
	started, _ := srv.Services.RegisterProcess(name, "umrc-bridge", 99)
	mrc.Description = "A test board"
	if rec := doJSON(t, h, http.MethodPut, "/api/doors/mrc", mrc, token); rec.Code != http.StatusOK {
		t.Fatalf("PUT: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got, _ := doors.ReadMRCConfig(dir); got.Description != "A test board" {
		t.Fatalf("mrc.cfg after PUT = %+v", got)
	}
	if !srv.Services.RestartRequested(name, started) {
		t.Fatal("saving the settings did not ask umrc-bridge to restart")
	}
	mrc.Port = "123456"
	if rec := doJSON(t, h, http.MethodPut, "/api/doors/mrc", mrc, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("too-long port: status = %d, want 400", rec.Code)
	}

	if rec := doJSON(t, h, http.MethodPost, "/api/services/"+url.PathEscape(name)+"/restart", nil, token); rec.Code != http.StatusAccepted {
		t.Fatalf("restarting the door program: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/services/door:nope/restart", nil, token); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown door program: status = %d, want 404", rec.Code)
	}
}

func TestValidateRLoginDoor(t *testing.T) {
	ok := []config.DoorConfig{{Name: "DoorParty", Kind: "rlogin", Remote: config.RemoteDoor{Host: "doors.example.net", ClientUser: "[NMB]{handle}"}}}
	if msg := validateDoors(ok); msg != "" {
		t.Fatalf("valid RLogin door refused: %s", msg)
	}
	noHost := []config.DoorConfig{{Name: "X", Kind: "rlogin", Remote: config.RemoteDoor{ClientUser: "u"}}}
	if msg := validateDoors(noHost); msg == "" {
		t.Fatal("RLogin door without a host accepted")
	}
	// Remote settings are kept only for an RLogin door.
	if d := fromDoorDTO(doorDTO{Name: "N", Kind: "native", Exe: "x", Dir: "d", Remote: config.RemoteDoor{Host: "h"}}); d.Remote.Host != "" {
		t.Fatal("a native door kept remote settings")
	}
}
