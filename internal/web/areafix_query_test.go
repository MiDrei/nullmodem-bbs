package web

import (
	"encoding/json"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/areafix"
)

func TestAreafixQueryAndAdoptingTheHubsWord(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()
	hub := binkpUplinkDTO{Address: "21:3/100", Host: "hub.example.com:24554", AreafixPassword: "pw1"}

	// An older reply (a %LIST answer) doesn't count for the query.
	srv.Netmail.Receive("Areafix", "21:3/100", 0, "Areafix", "", "Re: %LIST", " FSX_GEN  General\r\n FSX_ADS  Ads\r\n FSX_BOT  Bots\r\n", time.Now(), false)

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/areafix/query", areafixListRequestDTO{Uplink: hub, Kind: "echo"}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: %d %s", rec.Code, rec.Body)
	}
	pending, _ := srv.Netmail.PendingOutbound()
	if len(pending) != 1 || pending[0].Body != "%QUERY\r" || pending[0].ToName != "Areafix" {
		t.Fatalf("pending %+v", pending)
	}
	var reply queryReplyDTO
	json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/binkp/areafix/query-reply?address=21:3/100&kind=echo", nil, token).Body.Bytes(), &reply)
	if reply.Asked.IsZero() || reply.Found {
		t.Fatalf("before the answer: %+v", reply)
	}

	srv.Netmail.Receive("Areafix", "21:3/100", 0, "Areafix", "", "Re: %QUERY", "Following areas are linked to 21:3/194.1:\r\n\r\n FSX_GEN\r\n fsx_ads  Ads\r\n\r\n--- hpt\r\n", time.Now(), false)
	// The %LIST answer arriving after it, richer, isn't the query's.
	srv.Netmail.Receive("Areafix", "21:3/100", 0, "Areafix", "", "AREAFIX response", " FSX_GEN  General\r\n FSX_ADS  Ads\r\n FSX_BOT  Bots\r\n FSX_TST  Test\r\n", time.Now(), false)
	json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/binkp/areafix/query-reply?address=21:3/100&kind=echo&known=FSX_ADS,FSX_BOT", nil, token).Body.Bytes(), &reply)
	if !reply.Found || strings.Join(reply.Tags, ",") != "FSX_GEN,fsx_ads" {
		t.Fatalf("reply %+v", reply)
	}

	// Our record had FSX_BOT (refused by the hub, say); the hub's word wins.
	srv.EchoAreafix.Request(hub.Host, "FSX_BOT", areafix.Outbound)
	rec = doJSON(t, h, http.MethodPut, "/api/binkp/areafix/subscriptions", map[string]any{"host": hub.Host, "kind": "echo", "area_tags": reply.Tags}, token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"added":2`) || !strings.Contains(rec.Body.String(), `"removed":1`) {
		t.Fatalf("adopt: %d %s", rec.Code, rec.Body)
	}
	subs, _ := srv.EchoAreafix.ListForUplink(hub.Host, areafix.Outbound)
	var tags []string
	for _, s := range subs {
		tags = append(tags, strings.ToUpper(s.AreaTag))
	}
	if len(tags) != 2 || !strings.Contains(strings.Join(tags, ","), "FSX_GEN") || !strings.Contains(strings.Join(tags, ","), "FSX_ADS") {
		t.Fatalf("record %v", tags)
	}
	if pending, _ := srv.Netmail.PendingOutbound(); len(pending) != 1 {
		t.Fatalf("adopting sent something: %d", len(pending))
	}
}

func TestAreafixRepliesKeptApartByRobot(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()
	now := time.Now()
	srv.Netmail.Receive("Areafix", "1337:1/100", 0, "Sysop", "", "AREAFIX response", " TQW_GEN  General Chat\r\n TQW_ADS  BBS Adverts\r\n TQW_BOT  roBOT output\r\n", now, false)
	srv.Netmail.Receive("Filefix", "1337:1/100", 0, "Sysop", "", "FILEFIX response", " TQW_ANSI  ANSI art\r\n TQW_DEMOS  Demos\r\n", now, false)
	srv.Netmail.Receive("Filefix", "1337:1/100", 0, "Sysop", "", "Result", "<-- COMMAND PROCESSED\r\n+TQW_EBOOKS linked\r\n", now, false)

	tags := func(kind string) string {
		var resp struct {
			Areas []struct{ Tag string } `json:"areas"`
		}
		json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/binkp/areafix/list-reply?address=1337:1/100&kind="+kind, nil, token).Body.Bytes(), &resp)
		var out []string
		for _, a := range resp.Areas {
			out = append(out, a.Tag)
		}
		return strings.Join(out, ",")
	}
	if got := tags("echo"); got != "TQW_GEN,TQW_ADS,TQW_BOT" {
		t.Errorf("echo list = %s", got)
	}
	if got := tags("file"); got != "TQW_ANSI,TQW_DEMOS" {
		t.Errorf("file list = %s", got)
	}

	// The history: the robot's own replies and our requests, never the
	// password (a request's subject).
	doJSON(t, h, http.MethodPost, "/api/binkp/areafix/list", areafixListRequestDTO{
		Uplink: binkpUplinkDTO{Address: "1337:1/100", Host: "tqw:24554", FilefixPassword: "secret-ff"}, Kind: "file"}, token)
	rec := doJSON(t, h, http.MethodGet, "/api/binkp/areafix/history?address=1337:1/100&kind=file", nil, token)
	var hist []areafixHistoryDTO
	json.Unmarshal(rec.Body.Bytes(), &hist)
	if len(hist) != 3 || !hist[0].Outgoing || hist[0].Body != "%LIST\r" || strings.Contains(rec.Body.String(), "secret-ff") {
		t.Fatalf("history %s", rec.Body)
	}
	for _, e := range hist {
		if strings.Contains(e.Body, "TQW_GEN") {
			t.Errorf("Areafix reply in the Filefix history: %+v", e)
		}
	}
}

func TestAreafixRobotCommands(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()
	hub := binkpUplinkDTO{Address: "21:3/100", Host: "hub:24554", AreafixPassword: "a", FilefixPassword: "f"}
	send := func(kind, cmd string) int {
		return doJSON(t, h, http.MethodPost, "/api/binkp/areafix/command", map[string]any{"uplink": hub, "kind": kind, "command": cmd}, token).Code
	}
	if send("echo", "%HELP") != http.StatusOK || send("file", "%linked") != http.StatusOK {
		t.Fatal("known commands refused")
	}
	if send("echo", "+FSX_GEN") != http.StatusBadRequest || send("echo", "%DELETE ALL") != http.StatusBadRequest {
		t.Fatal("other commands accepted")
	}
	pending, _ := srv.Netmail.PendingOutbound()
	if len(pending) != 2 || pending[0].Body != "%HELP\r" || pending[0].ToName != "Areafix" || pending[1].Body != "%LINKED\r" || pending[1].ToName != "Filefix" {
		t.Fatalf("pending %+v", pending)
	}
	// %LINKED counts as asking what's linked.
	var reply queryReplyDTO
	json.Unmarshal(doJSON(t, h, http.MethodGet, "/api/binkp/areafix/query-reply?address=21:3/100&kind=file", nil, token).Body.Bytes(), &reply)
	if reply.Asked.IsZero() {
		t.Fatal("%LINKED didn't count as a query")
	}
}

func TestAreafixHistoryHidesPasswords(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()
	c, _ := config.Load(srv.BBSConfigPath)
	c.Binkp.Uplinks = []config.BinkpUplink{{Address: "21:3/100", Host: "hub:24554", AreafixPassword: "GERTSECRET"}}
	config.Save(srv.BBSConfigPath, c)
	srv.Netmail.Receive("Clearing Houz", "21:3/100", 0, "Sysop", "", "Areafix - Result", "%LIST <-- COMMAND PROCESSED\r\nSUBJECT: GERTSECRET\r\n", time.Now(), false)
	rec := doJSON(t, h, http.MethodGet, "/api/binkp/areafix/history?address=21:3/100&kind=echo", nil, token)
	if strings.Contains(rec.Body.String(), "GERTSECRET") || !strings.Contains(rec.Body.String(), "COMMAND PROCESSED") {
		t.Fatalf("history %s", rec.Body)
	}
}
