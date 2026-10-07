package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestConfigNetworksValidateAndRenameCarriesOver(t *testing.T) {
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatal(err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	base := func() configDTO {
		return configDTO{
			Name: "X", Sysop: "root", NewUserSL: 10, TelnetEnabled: true, TelnetAddr: ":2323",
			Networks:     []networkDTO{{Name: "fsxNet", Domain: "fsxnet"}},
			BinkpUplinks: []binkpUplinkDTO{{Address: "21:3/100", Host: "hub:24554", Network: "fsxNet"}},
		}
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/config", base(), token); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	area, err := srv.Messages.CreateArea("FSX_GEN", "FSX_GEN", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	for name, dto := range map[string]configDTO{
		"undefined uplink network": func() configDTO { d := base(); d.BinkpUplinks[0].Network = "FidoNet"; return d }(),
		"duplicate network": func() configDTO {
			d := base()
			d.Networks = append(d.Networks, networkDTO{Name: "FSXNET", Domain: "x"})
			return d
		}(),
		"bad domain": func() configDTO { d := base(); d.Networks[0].Domain = "fsx net"; return d }(),
		"empty name": func() configDTO { d := base(); d.Networks[0].Name = " "; return d }(),
	} {
		if rec := doJSON(t, h, http.MethodPut, "/api/config", dto, token); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}

	// Rename fsxNet -> FSX: uplink and area follow.
	rename := base()
	rename.Networks[0] = networkDTO{Name: "FSX", Domain: "fsxnet", OriginalName: "fsxNet"}
	rec := doJSON(t, h, http.MethodPut, "/api/config", rename, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	c, _ := config.Load(configPath)
	if c.Networks[0].Name != "FSX" || c.Binkp.Uplinks[0].Network != "FSX" {
		t.Fatalf("config after rename: networks=%+v uplink network=%q", c.Networks, c.Binkp.Uplinks[0].Network)
	}
	got, err := srv.Messages.AreaByID(area.ID)
	if err != nil || got.Network != "FSX" {
		t.Fatalf("area network after rename = %q (%v), want FSX", got.Network, err)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/groups", nil, token)
	var groups []string
	json.Unmarshal(rec.Body.Bytes(), &groups)
	if len(groups) != 1 || groups[0] != "FSX" {
		t.Fatalf("groups = %v, want [FSX]", groups)
	}
}
