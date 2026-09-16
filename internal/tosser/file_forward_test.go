package tosser

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/tic"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestRoutedOutboundFileForwardReturnsSubscribedAreaFiles(t *testing.T) {
	_, _, files, users, _, fileSubs := newTestStoresWithRobot(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := files.CreateArea("FSX_FILES", "fsxNet Files", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := files.UploadFile(area.ID, u.ID, "readme.txt", "a file", strings.NewReader("hello")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if err := fileSubs.Request(downlinkUplink.Host, "FSX_FILES", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	out, err := RoutedOutboundFileForward(files, fileSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundFileForward: %v", err)
	}
	if len(out) != 1 || out[0].AreaTag != "FSX_FILES" || out[0].Filename != "readme.txt" {
		t.Fatalf("RoutedOutboundFileForward = %+v, want the one file in FSX_FILES", out)
	}
}

func TestRoutedOutboundFileForwardSkipsFilesAlreadySeenByTarget(t *testing.T) {
	_, _, files, users, _, fileSubs := newTestStoresWithRobot(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := files.CreateArea("FSX_FILES", "fsxNet Files", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	f, err := files.UploadFile(area.ID, u.ID, "readme.txt", "", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if err := fileSubs.Request(downlinkUplink.Host, "FSX_FILES", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if err := files.MarkSeenBy(f.ID, "3/100"); err != nil {
		t.Fatalf("MarkSeenBy: %v", err)
	}

	out, err := RoutedOutboundFileForward(files, fileSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundFileForward: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("RoutedOutboundFileForward = %+v, want empty -- target already has this file", out)
	}
}

func TestRoutedOutboundFileForwardSkipsUnsubscribedAreas(t *testing.T) {
	_, _, files, users, _, fileSubs := newTestStoresWithRobot(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := files.CreateArea("FSX_FILES", "fsxNet Files", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := files.UploadFile(area.ID, u.ID, "readme.txt", "", strings.NewReader("hello")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	out, err := RoutedOutboundFileForward(files, fileSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundFileForward: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("RoutedOutboundFileForward = %+v, want empty -- no subscription", out)
	}
}

func TestRoutedOutboundFileForwardSkipsSubscriptionForDeletedArea(t *testing.T) {
	_, _, files, _, _, fileSubs := newTestStoresWithRobot(t)
	if err := fileSubs.Request(downlinkUplink.Host, "GHOST_AREA", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	out, err := RoutedOutboundFileForward(files, fileSubs, downlinkUplink)
	if err != nil {
		t.Fatalf("RoutedOutboundFileForward: %v, want a subscription for a nonexistent area to be skipped, not an error", err)
	}
	if len(out) != 0 {
		t.Fatalf("RoutedOutboundFileForward = %+v, want empty", out)
	}
}

func TestEncodeTICIncludesPasswordOnlyWhenSet(t *testing.T) {
	pf := PendingFileForward{File: file.File{Filename: "readme.txt"}, AreaTag: "FSX_FILES"}
	ourAddr := mail.Address{Zone: 21, Net: 3, Node: 1}

	withPw := string(encodeTIC(pf, ourAddr, "secret1", 5, 0xDEADBEEF))
	if !strings.Contains(withPw, "Pw secret1") {
		t.Fatalf("encodeTIC with a password = %q, want a Pw line", withPw)
	}

	withoutPw := string(encodeTIC(pf, ourAddr, "", 5, 0xDEADBEEF))
	if strings.Contains(withoutPw, "Pw ") {
		t.Fatalf("encodeTIC with no password = %q, want no Pw line", withoutPw)
	}
	if !strings.Contains(withoutPw, "Area FSX_FILES") || !strings.Contains(withoutPw, "File readme.txt") {
		t.Fatalf("encodeTIC = %q, want Area and File lines", withoutPw)
	}
}

func TestEncodeTICRoundTripsThroughParse(t *testing.T) {
	pf := PendingFileForward{
		File:    file.File{Filename: "readme.txt", Description: "First line\nSecond line"},
		AreaTag: "FSX_FILES",
	}
	ourAddr := mail.Address{Zone: 21, Net: 3, Node: 1}
	data := encodeTIC(pf, ourAddr, "secret1", 12345, 0xA1B2C3D4)

	parsed, err := tic.Parse(data)
	if err != nil {
		t.Fatalf("tic.Parse(encodeTIC(...)): %v", err)
	}
	if parsed.Area != "FSX_FILES" || parsed.Name != "readme.txt" {
		t.Fatalf("parsed = %+v, want Area FSX_FILES / Name readme.txt", parsed)
	}
	if parsed.SizeBytes != 12345 {
		t.Fatalf("parsed.SizeBytes = %d, want 12345", parsed.SizeBytes)
	}
	if !parsed.HasCRC32 || parsed.CRC32 != 0xA1B2C3D4 {
		t.Fatalf("parsed CRC32/HasCRC32 = %08X/%v, want A1B2C3D4/true", parsed.CRC32, parsed.HasCRC32)
	}
	if parsed.Password != "secret1" {
		t.Fatalf("parsed.Password = %q, want %q", parsed.Password, "secret1")
	}
	if parsed.Description != "First line\nSecond line" {
		t.Fatalf("parsed.Description = %q, want both lines preserved", parsed.Description)
	}
}

// TestPollForwardsFileToSubscribedDownlinkAndMarksSeenBy is an
// end-to-end regression test through Poll itself: a downlink with an
// active inbound Filefix subscription must receive both a TIC
// descriptor and the file's own bytes for a file in that area, and
// the file's SeenBy must afterward list the downlink so it isn't sent
// again.
func TestPollForwardsFileToSubscribedDownlinkAndMarksSeenBy(t *testing.T) {
	netmailStore, messages, files, users, echoSubs, fileSubs := newTestStoresWithRobot(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := files.CreateArea("FSX_FILES", "fsxNet Files", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	uploaded, err := files.UploadFile(area.ID, u.ID, "readme.txt", "a test file", strings.NewReader("hello downlink"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	var mu sync.Mutex
	received := map[string][]byte{}
	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{downlinkUplink.Address},
		ReceiveFile: func(f binkp.InboundFile, r io.Reader) error {
			data, err := io.ReadAll(r)
			mu.Lock()
			received[f.Name] = data
			mu.Unlock()
			return err
		},
	})

	dial := downlinkUplink
	dial.Host = addr
	dial.TICPassword = "filesecret"
	if err := fileSubs.Request(dial.Host, "FSX_FILES", areafix.Inbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	robot := &RobotConfig{
		OurAddresses: []string{"21:3/194.1"},
		Uplinks:      []config.BinkpUplink{dial},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, "Test BBS", dial, []config.BinkpUplink{dial}, netmailStore, messages, users, robot, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.ForwardedFiles != 1 {
		t.Fatalf("Result.ForwardedFiles = %d, want 1", res.ForwardedFiles)
	}

	mu.Lock()
	defer mu.Unlock()
	payload, ok := received["readme.txt"]
	if !ok || string(payload) != "hello downlink" {
		t.Fatalf("received[readme.txt] = %q, ok=%v, want the original content", payload, ok)
	}

	var ticData []byte
	for name, data := range received {
		if strings.HasSuffix(name, ".tic") {
			ticData = data
		}
	}
	if ticData == nil {
		t.Fatalf("no .tic file received, got: %v", received)
	}
	parsed, err := tic.Parse(ticData)
	if err != nil {
		t.Fatalf("tic.Parse: %v", err)
	}
	if parsed.Area != "FSX_FILES" || parsed.Name != "readme.txt" {
		t.Fatalf("parsed TIC = %+v, want Area FSX_FILES / Name readme.txt", parsed)
	}
	if parsed.Password != "filesecret" {
		t.Fatalf("parsed TIC Password = %q, want %q", parsed.Password, "filesecret")
	}

	reloaded, err := files.FileByID(uploaded.ID)
	if err != nil {
		t.Fatalf("FileByID: %v", err)
	}
	if !file.SeenByNetNodes(reloaded.SeenBy)["3/100"] {
		t.Fatalf("reloaded.SeenBy = %q, want it to now list the downlink (3/100)", reloaded.SeenBy)
	}

	out2, err := RoutedOutboundFileForward(files, fileSubs, dial)
	if err != nil {
		t.Fatalf("RoutedOutboundFileForward after send: %v", err)
	}
	if len(out2) != 0 {
		t.Fatalf("RoutedOutboundFileForward after send = %+v, want empty (already delivered)", out2)
	}
}
