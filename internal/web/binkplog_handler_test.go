package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestListBinkpSessionsReturnsRecordedEntries(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r, err := srv.BinkpLog.Begin("outbound", "21:3/194", "host:24554")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	r.RecordFrame("send", "M_ADR 21:3/194")
	r.RecordFrame("recv", "M_PWD ***")
	if _, err := r.Finish("ok", ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/binkp/sessions", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var entries []binkpSessionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 || entries[0].Direction != "outbound" || entries[0].PeerAddress != "21:3/194" || entries[0].Outcome != "ok" {
		t.Fatalf("entries = %+v, want one outbound 21:3/194 ok entry", entries)
	}
}

func TestGetBinkpSessionTranscriptReturnsRecordedLines(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r, err := srv.BinkpLog.Begin("inbound", "1337:1/131", "host:24554")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	r.RecordFrame("recv", "M_ADR 1337:1/131")
	entry, err := r.Finish("ok", "")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/binkp/sessions/"+strconv.FormatInt(entry.ID, 10)+"/transcript", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "M_ADR 1337:1/131") {
		t.Fatalf("transcript = %q, want it to contain the recorded line", rec.Body.String())
	}
}

func TestGetBinkpSessionTranscriptReturns404ForUnknownID(t *testing.T) {
	srv, users, _ := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")

	rec := doJSON(t, h, http.MethodGet, "/api/binkp/sessions/999/transcript", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET status = %d, want 404", rec.Code)
	}
}
