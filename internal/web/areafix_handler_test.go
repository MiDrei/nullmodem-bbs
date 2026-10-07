package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/areafix"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func setupAreafixTest(t *testing.T) (*Server, string) {
	t.Helper()
	srv, users, configPath := newTestServer(t)
	if _, err := users.Register("root", "supersecret", user.SLSysop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	c.BBS.FTNAddresses = []string{"21:3/194.1"}
	if err := config.Save(configPath, c); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	h := srv.Routes()
	token := loginAsSysop(t, h, "root", "supersecret")
	return srv, token
}

func TestRequestAreafixChangesBatchesAndRecordsMultipleAreas(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/areafix/changes", areafixChangesRequestDTO{
		Uplink: binkpUplinkDTO{
			Address:         "21:3/100",
			Host:            "hub.example.com:24554",
			AreafixPassword: "secret1",
		},
		Changes: []areaChangeDTO{
			{AreaTag: "FSX_GEN", Subscribe: true},
			{AreaTag: "FSX_ADS", Subscribe: true},
			{AreaTag: "FSX_OLD", Subscribe: false},
		},
		Kind: "echo",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	pending, err := srv.Netmail.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 1 || pending[0].ToName != "Areafix" {
		t.Fatalf("pending netmail = %+v, want exactly one message to Areafix (batched)", pending)
	}

	subs, err := srv.EchoAreafix.ListForUplink("hub.example.com:24554", areafix.Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("recorded subscriptions = %+v, want 2 (FSX_OLD withdrawn, not recorded)", subs)
	}
}

func TestRequestAreafixChangesFileKindUsesFilefix(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/areafix/changes", areafixChangesRequestDTO{
		Uplink: binkpUplinkDTO{
			Address:         "21:3/100",
			Host:            "hub.example.com:24554",
			FilefixPassword: "filesecret",
		},
		Changes: []areaChangeDTO{{AreaTag: "FSX_FILES", Subscribe: true}},
		Kind:    "file",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	pending, err := srv.Netmail.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 1 || pending[0].ToName != "Filefix" {
		t.Fatalf("pending netmail = %+v, want one message to Filefix", pending)
	}
}

func TestRequestAreafixChangesRejectsEmptyChangeList(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/areafix/changes", areafixChangesRequestDTO{
		Uplink:  binkpUplinkDTO{Address: "21:3/100", Host: "hub.example.com:24554"},
		Changes: nil,
	}, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (empty changes)", rec.Code)
	}
}

func TestRequestAreafixListQueuesListCommand(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodPost, "/api/binkp/areafix/list", areafixListRequestDTO{
		Uplink: binkpUplinkDTO{Address: "21:3/100", Host: "hub.example.com:24554", AreafixPassword: "pw1"},
		Kind:   "echo",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	pending, err := srv.Netmail.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 1 || pending[0].Subject != "pw1" || pending[0].Body != "%LIST\r" {
		t.Fatalf("pending netmail = %+v, want one %%LIST request with the password as Subject", pending)
	}
}

func TestGetAreafixListReplyParsesLatestReply(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()

	written := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	if _, err := srv.Netmail.Receive("Areafix", "21:3/100", 0, "Areafix", "", "Re: %LIST", " FSX_GEN   fsxNet General\r\n+FSX_ADS   fsxNet Ads\r\n", written, false); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/binkp/areafix/list-reply?address=21:3/100", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Found bool `json:"found"`
		Areas []struct {
			Tag        string `json:"Tag"`
			Subscribed bool   `json:"Subscribed"`
		} `json:"areas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Found {
		t.Fatal("found = false, want true")
	}
	if len(resp.Areas) != 2 || resp.Areas[0].Tag != "FSX_GEN" || resp.Areas[1].Tag != "FSX_ADS" || !resp.Areas[1].Subscribed {
		t.Fatalf("parsed areas = %+v, want FSX_GEN (unsubscribed) and FSX_ADS (subscribed)", resp.Areas)
	}
}

// TestGetAreafixListReplyPicksTheRicherOfTwoSameTimestampReplies is a
// regression test for a real production bug: a real hub ("Clearing
// Houz") replies to a "%LIST" request with TWO separate netmail
// messages carrying the identical timestamp -- one actually listing
// the areas, one just an empty "COMMAND PROCESSED" confirmation --
// and picking by "most recent" alone landed on whichever of the two
// happened to sort last, the confirmation as often as the real list.
func TestGetAreafixListReplyPicksTheRicherOfTwoSameTimestampReplies(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()

	written := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	if _, err := srv.Netmail.Receive("Clearing Houz", "21:3/100", 0, "Areafix", "", "Areafix - List", " FSX_GEN   fsxNet General\r\n+FSX_ADS   fsxNet Ads\r\n", written, false); err != nil {
		t.Fatalf("Receive (list): %v", err)
	}
	if _, err := srv.Netmail.Receive("Clearing Houz", "21:3/100", 0, "Areafix", "", "Areafix - Result", "%LIST                     <-- COMMAND PROCESSED\r\n", written, false); err != nil {
		t.Fatalf("Receive (result): %v", err)
	}

	rec := doJSON(t, h, http.MethodGet, "/api/binkp/areafix/list-reply?address=21:3/100", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Found   bool   `json:"found"`
		Subject string `json:"subject"`
		Areas   []struct {
			Tag string `json:"Tag"`
		} `json:"areas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Found || resp.Subject != "Areafix - List" || len(resp.Areas) != 2 {
		t.Fatalf("got subject=%q areas=%+v, want the \"Areafix - List\" reply with 2 areas, not the confirmation", resp.Subject, resp.Areas)
	}
}

func TestGetAreafixListReplyNotFoundWhenNothingReceived(t *testing.T) {
	srv, token := setupAreafixTest(t)
	h := srv.Routes()

	rec := doJSON(t, h, http.MethodGet, "/api/binkp/areafix/list-reply?address=21:3/999", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Found bool `json:"found"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Found {
		t.Fatal("found = true, want false (nothing ever received from that address)")
	}
}

func TestListAreafixSubscriptionsReturnsRecordedTags(t *testing.T) {
	srv, token := setupAreafixTest(t)
	if err := srv.EchoAreafix.Request("hub.example.com:24554", "FSX_GEN", areafix.Outbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	h := srv.Routes()
	rec := doJSON(t, h, http.MethodGet, "/api/binkp/areafix/subscriptions?host=hub.example.com:24554&kind=echo", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		AreaTags []string `json:"area_tags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.AreaTags) != 1 || resp.AreaTags[0] != "FSX_GEN" {
		t.Fatalf("area_tags = %v, want [FSX_GEN]", resp.AreaTags)
	}
}

func TestListAreafixGrantsShowsAllLocalAreasWithGrantedState(t *testing.T) {
	srv, token := setupAreafixTest(t)
	if _, err := srv.Messages.CreateArea("FSX_GEN", "fsxNet General", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := srv.Messages.CreateArea("FSX_ADS", "fsxNet Ads", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if err := srv.EchoAreafix.Grant("downlink.example.com:24554", "FSX_GEN"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	h := srv.Routes()
	rec := doJSON(t, h, http.MethodGet, "/api/binkp/areafix/grants?host=downlink.example.com:24554&kind=echo", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Areas []areaGrantDTO `json:"areas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byTag := map[string]areaGrantDTO{}
	for _, a := range resp.Areas {
		byTag[a.Tag] = a
	}
	if !byTag["FSX_GEN"].Granted {
		t.Fatalf("FSX_GEN = %+v, want Granted true", byTag["FSX_GEN"])
	}
	if byTag["FSX_ADS"].Granted {
		t.Fatalf("FSX_ADS = %+v, want Granted false (never granted)", byTag["FSX_ADS"])
	}
	// The seeded default "general" area must also be listed -- every
	// local area is shown, not just ones already granted.
	if _, ok := byTag["general"]; !ok {
		t.Fatalf("areas = %+v, want the seeded default area included too", resp.Areas)
	}
}

func TestSetAreafixGrantsReplacesTheFullGrantedSet(t *testing.T) {
	srv, token := setupAreafixTest(t)
	if _, err := srv.Messages.CreateArea("FSX_GEN", "fsxNet General", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := srv.Messages.CreateArea("FSX_ADS", "fsxNet Ads", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	// Pre-existing grant for an area that's about to be left out of
	// the new set -- must end up revoked.
	if err := srv.EchoAreafix.Grant("downlink.example.com:24554", "FSX_ADS"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	h := srv.Routes()
	rec := doJSON(t, h, http.MethodPut, "/api/binkp/areafix/grants", setAreafixGrantsRequestDTO{
		Host:        "downlink.example.com:24554",
		Kind:        "echo",
		GrantedTags: []string{"FSX_GEN"},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	granted, err := srv.EchoAreafix.IsGranted("downlink.example.com:24554", "FSX_GEN")
	if err != nil {
		t.Fatalf("IsGranted FSX_GEN: %v", err)
	}
	if !granted {
		t.Fatal("FSX_GEN not granted after PUT, want it granted")
	}
	granted, err = srv.EchoAreafix.IsGranted("downlink.example.com:24554", "FSX_ADS")
	if err != nil {
		t.Fatalf("IsGranted FSX_ADS: %v", err)
	}
	if granted {
		t.Fatal("FSX_ADS still granted after PUT, want it revoked (left out of the new set)")
	}
}

func TestSetAreafixGrantsFileKindUsesFilefixStore(t *testing.T) {
	srv, token := setupAreafixTest(t)
	if _, err := srv.Files.CreateArea("FSX_FILES", "fsxNet Files", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	h := srv.Routes()
	rec := doJSON(t, h, http.MethodPut, "/api/binkp/areafix/grants", setAreafixGrantsRequestDTO{
		Host:        "downlink.example.com:24554",
		Kind:        "file",
		GrantedTags: []string{"FSX_FILES"},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	granted, err := srv.FileAreafix.IsGranted("downlink.example.com:24554", "FSX_FILES")
	if err != nil {
		t.Fatalf("IsGranted: %v", err)
	}
	if !granted {
		t.Fatal("FSX_FILES not granted after PUT via the file kind")
	}
	// Must not have touched the echo grant store.
	granted, err = srv.EchoAreafix.IsGranted("downlink.example.com:24554", "FSX_FILES")
	if err != nil {
		t.Fatalf("IsGranted (echo store): %v", err)
	}
	if granted {
		t.Fatal("FSX_FILES granted in the echo store too, want the file kind to only touch FileAreafix")
	}
}
