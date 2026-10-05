package web

import (
	"encoding/json"
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
