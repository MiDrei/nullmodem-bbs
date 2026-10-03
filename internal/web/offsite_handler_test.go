package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestOffsiteSettingsKeepSecrets(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	users.Register("root", "supersecret", user.SLSysop)
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	var keys map[string]string
	json.Unmarshal(doJSON(t, h, http.MethodPost, "/api/backups/offsite/age-key", nil, token).Body.Bytes(), &keys)
	if !strings.HasPrefix(keys["private"], "AGE-SECRET-KEY-") || !strings.HasPrefix(keys["public"], "age1") {
		t.Fatalf("keys %v", keys)
	}
	settings := map[string]any{"enabled": true, "kind": "swift", "recipient": keys["public"], "keep_daily": 14, "keep_weekly": 8,
		"swift_auth_url": "https://swiss-backup02.infomaniak.com/identity/v3", "swift_user": "SBI-XY", "swift_password": "pw-swift",
		"swift_project": "sb_project_SBI-XY", "swift_container": "bbs"}
	if rec := doJSON(t, h, http.MethodPut, "/api/backups/offsite", map[string]any{"enabled": true, "kind": "swift", "keep_daily": 1}, token); rec.Code != http.StatusBadRequest {
		t.Errorf("on without a key: %d", rec.Code)
	}
	rec := doJSON(t, h, http.MethodPut, "/api/backups/offsite", settings, token)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "pw-swift") || !strings.Contains(rec.Body.String(), `"swift_has_password":true`) {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	// Saving again without the password keeps it.
	delete(settings, "swift_password")
	doJSON(t, h, http.MethodPut, "/api/backups/offsite", settings, token)
	c, _ := config.Load(configPath)
	if c.Backup.Offsite.Swift.Password != "pw-swift" || c.Backup.Offsite.Recipient != keys["public"] {
		t.Fatalf("config %+v", c.Backup.Offsite)
	}
	// The SFTP login key: kept, only its public line shown.
	rec = doJSON(t, h, http.MethodPost, "/api/backups/offsite/ssh-key", nil, token)
	if !strings.Contains(rec.Body.String(), `"sftp_public_key":"ssh-ed25519 `) || strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Fatalf("ssh key: %s", rec.Body)
	}
}
