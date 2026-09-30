package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestMaintenanceSettingsPreviewAndRun(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/maintenance", nil, token)
	var got maintenanceResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.Settings.Enabled || got.Settings.MessageKeepDays != 365 || got.Settings.DataAreaKeepDays != 30 ||
		got.Settings.Hour != 4 || got.Settings.LogKeepRows != 5000 || !got.Settings.Vacuum || got.Last != nil {
		t.Fatalf("defaults: %d %+v", rec.Code, got)
	}

	area, _ := srv.Messages.CreateArea("OLD", "Old", "", "", 0, 0)
	srv.Messages.ReceiveEcho(area.ID, "x", "ancient", "b", "", time.Now().AddDate(-2, 0, 0))

	rec = doJSON(t, h, http.MethodPost, "/api/maintenance/run", map[string]bool{"dry": true}, token)
	var preview struct{ Messages int }
	json.Unmarshal(rec.Body.Bytes(), &preview)
	if rec.Code != http.StatusOK || preview.Messages != 1 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	var afterPreview maintenanceResponse
	json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/maintenance", nil, token).Body.Bytes(), &afterPreview)
	if afterPreview.Last != nil {
		t.Fatal("a preview was recorded as a run")
	}

	settings := got.Settings
	settings.Enabled, settings.MessageKeepDays = true, 500
	if rec := doJSON(t, h, http.MethodPut, "/api/maintenance", settings, token); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	c, _ := config.Load(configPath)
	if !c.Maintenance.Enabled || c.Maintenance.MessageDays() != 500 {
		t.Fatalf("saved: %+v", c.Maintenance)
	}
	bad := settings
	bad.Hour = 24
	if rec := doJSON(t, h, http.MethodPut, "/api/maintenance", bad, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("hour 24: %d", rec.Code)
	}

	rec = doJSON(t, h, http.MethodPost, "/api/maintenance/run", map[string]bool{"dry": false}, token)
	var run struct{ Messages int }
	json.Unmarshal(rec.Body.Bytes(), &run)
	if rec.Code != http.StatusOK || run.Messages != 1 {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, "/api/maintenance", nil, token)
	var after maintenanceResponse
	json.Unmarshal(rec.Body.Bytes(), &after)
	if after.Last == nil || after.Last.Messages != 1 {
		t.Fatalf("last run not recorded: %+v", after.Last)
	}
}
