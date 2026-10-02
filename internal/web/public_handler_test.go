package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPublicOverviewNeedsNoLogin(t *testing.T) {
	srv, users, _ := newTestServer(t)
	publicCached = nil
	u, err := users.Register("Caller", "secret12", 10)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := srv.DB.Exec(`UPDATE users SET last_login_at = CURRENT_TIMESTAMP, total_calls = 3, location = 'Bern' WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv.Routes(), http.MethodGet, "/api/public/overview", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var o publicOverviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if o.Name == "" || o.Version == "" {
		t.Errorf("name/version missing: %+v", o)
	}
	if len(o.Callers) != 1 || o.Callers[0].Handle != "Caller" || o.Callers[0].Place != "Bern" {
		t.Errorf("callers = %+v", o.Callers)
	}
	if o.SinceYear < 2000 {
		t.Errorf("since_year = %d", o.SinceYear)
	}
	if o.Stats["users"] != 1 || o.Stats["calls"] != 3 {
		t.Errorf("stats = %v", o.Stats)
	}
}
