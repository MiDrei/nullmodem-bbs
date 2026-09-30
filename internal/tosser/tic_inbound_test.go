package tosser

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/mail"
	"hash/crc32"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/binkp"
	"git.maik.ch/nullmodem/bbs/internal/config"
)

// buildTICBytes renders a minimal .tic descriptor's contents from the
// given keyword lines, mirroring exactly what a real hub would send
// -- see internal/tic's own tests for the parser side of this.
func buildTICBytes(lines ...string) []byte {
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

func TestTICSessionTossesAFileWhenTICArrivesFirst(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip", "Desc a file")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if res.ReceivedFiles != 0 {
		t.Fatalf("ReceivedFiles = %d after the TIC alone, want 0 (payload not seen yet)", res.ReceivedFiles)
	}

	if err := ts.receive("readme.zip", strings.NewReader("file contents"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 1 {
		t.Fatalf("ReceivedFiles = %d after the payload arrived, want 1", res.ReceivedFiles)
	}

	area, err := files.AreaByTag("FSX_FILES")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if !area.Pending {
		t.Fatal("auto-created area Pending = false, want true (mirrors tossEcho's echomail auto-create)")
	}
	stored, err := files.ListFiles(area.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(stored) != 1 || stored[0].Filename != "readme.zip" {
		t.Fatalf("stored files = %+v, want exactly one named readme.zip", stored)
	}
}

// buildZipWithFileIDDiz builds an in-memory .zip containing a
// FILE_ID.DIZ entry with the given text plus one other, unrelated
// file -- mirroring a real distribution .zip's shape.
func buildZipWithFileIDDiz(t *testing.T, dizText string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	diz, err := zw.Create("FILE_ID.DIZ")
	if err != nil {
		t.Fatalf("zip Create FILE_ID.DIZ: %v", err)
	}
	if _, err := diz.Write([]byte(dizText)); err != nil {
		t.Fatalf("zip write FILE_ID.DIZ: %v", err)
	}
	payload, err := zw.Create("apod0923.jpg")
	if err != nil {
		t.Fatalf("zip Create payload: %v", err)
	}
	if _, err := payload.Write([]byte("not really a jpeg")); err != nil {
		t.Fatalf("zip write payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

func TestTICSessionUsesFileIDDizAsDescriptionWhenTICHasNone(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	zipData := buildZipWithFileIDDiz(t, "Astronomy Picture of the Day\r\nDaily image feed")

	// No Desc/AreaDesc/Ldesc line at all -- exactly what was observed
	// live from a real fsxNet apodNNNN.zip distribution.
	ticData := buildTICBytes("Area FSX_IMGE", "File apod0923.zip")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("apod0923.zip", bytes.NewReader(zipData), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 1 {
		t.Fatalf("ReceivedFiles = %d, want 1", res.ReceivedFiles)
	}

	area, err := files.AreaByTag("FSX_IMGE")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	stored, err := files.ListFiles(area.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored files = %+v, want exactly one", stored)
	}
	want := "Astronomy Picture of the Day\r\nDaily image feed"
	if stored[0].Description != want {
		t.Fatalf("Description = %q, want the FILE_ID.DIZ text %q", stored[0].Description, want)
	}
}

func TestTICSessionPrefersTICDescriptionOverFileIDDiz(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	zipData := buildZipWithFileIDDiz(t, "DIZ text, should be ignored")

	ticData := buildTICBytes("Area FSX_IMGE", "File apod0923.zip", "Desc a real TIC description")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("apod0923.zip", bytes.NewReader(zipData), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}

	area, err := files.AreaByTag("FSX_IMGE")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	stored, err := files.ListFiles(area.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(stored) != 1 || stored[0].Description != "a real TIC description" {
		t.Fatalf("stored = %+v, want the TIC's own description kept, not the DIZ", stored)
	}
}

func TestTICSessionTossesAFileWhenPayloadArrivesFirst(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	if err := ts.receive("readme.zip", strings.NewReader("file contents"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 0 {
		t.Fatalf("ReceivedFiles = %d after the payload alone, want 0 (TIC not seen yet)", res.ReceivedFiles)
	}

	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if res.ReceivedFiles != 1 {
		t.Fatalf("ReceivedFiles = %d after the TIC arrived, want 1 -- order must not matter", res.ReceivedFiles)
	}
}

func TestTICSessionMatchesFilenameCaseInsensitively(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	ticData := buildTICBytes("Area FSX_FILES", "File ReadMe.ZIP")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("readme.zip", strings.NewReader("x"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 1 {
		t.Fatalf("ReceivedFiles = %d, want 1 -- filenames must correlate case-insensitively", res.ReceivedFiles)
	}
}

func TestTICSessionRejectsWrongPassword(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, []string{"secret1"})
	res := &Result{}

	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip", "Pw wrongpass")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("readme.zip", strings.NewReader("x"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 0 {
		t.Fatalf("ReceivedFiles = %d, want 0 for a wrong TIC password", res.ReceivedFiles)
	}
	if len(res.SkippedFiles) != 1 {
		t.Fatalf("SkippedFiles = %v, want the rejected file reported", res.SkippedFiles)
	}
}

func TestTICSessionAcceptsCorrectPasswordCaseInsensitively(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, []string{"Secret1"})
	res := &Result{}

	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip", "Pw SECRET1")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("readme.zip", strings.NewReader("x"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 1 {
		t.Fatalf("ReceivedFiles = %d, want 1 -- TIC passwords are compared case-insensitively", res.ReceivedFiles)
	}
}

func TestTICSessionRejectsSizeMismatch(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip", "Size 999")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("readme.zip", strings.NewReader("short"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 0 {
		t.Fatalf("ReceivedFiles = %d, want 0 for a claimed size that doesn't match the received payload", res.ReceivedFiles)
	}
	if len(res.SkippedFiles) != 1 {
		t.Fatalf("SkippedFiles = %v, want the rejected file reported", res.SkippedFiles)
	}
}

func TestTICSessionRejectsCRCMismatch(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip", "Crc DEADBEEF")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("readme.zip", strings.NewReader("actual content"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 0 {
		t.Fatalf("ReceivedFiles = %d, want 0 for a CRC-32 that doesn't match the received payload", res.ReceivedFiles)
	}
}

func TestTICSessionAcceptsMatchingCRC(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	payload := "actual content"
	crc := crc32.ChecksumIEEE([]byte(payload))
	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip", fmt.Sprintf("Crc %08X", crc))
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("readme.zip", strings.NewReader(payload), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if res.ReceivedFiles != 1 {
		t.Fatalf("ReceivedFiles = %d, want 1 for a matching CRC-32", res.ReceivedFiles)
	}
}

func TestTICSessionFlushUnmatchedReportsLeftovers(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	ticData := buildTICBytes("Area FSX_FILES", "File never-arrives.zip")
	if err := ts.receive("00001122.tic", bytes.NewReader(ticData), res); err != nil {
		t.Fatalf("receive TIC: %v", err)
	}
	if err := ts.receive("orphan-payload.zip", strings.NewReader("x"), res); err != nil {
		t.Fatalf("receive payload: %v", err)
	}
	if len(res.SkippedFiles) != 0 {
		t.Fatalf("SkippedFiles before flush = %v, want empty (both halves still might arrive)", res.SkippedFiles)
	}

	ts.flushUnmatched(res)
	if len(res.SkippedFiles) != 2 {
		t.Fatalf("SkippedFiles after flush = %v, want 2 (the unmatched TIC and the unmatched payload)", res.SkippedFiles)
	}
}

func TestTICSessionMalformedTICIsSkippedNotFatal(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	ts := newTICSession(files, nil)
	res := &Result{}

	if err := ts.receive("broken.tic", strings.NewReader("not a valid tic file"), res); err != nil {
		t.Fatalf("receive: %v, want nil (a malformed TIC is reported, not a fatal error)", err)
	}
	if len(res.SkippedFiles) != 1 {
		t.Fatalf("SkippedFiles = %v, want the malformed file reported", res.SkippedFiles)
	}
}

// TestPollTossesFileFromRealBinkpSession is an end-to-end regression
// test confirming the real wiring (Poll -> handleInboundFile ->
// ticSession) works, not just ticSession in isolation: a fake uplink
// sends both a .tic descriptor and its payload as two separate
// OutboundFiles in one real BinkP session (order matching how a real
// hub sends them), and the file must land in the local file area.
func TestPollTossesFileFromRealBinkpSession(t *testing.T) {
	netmailStore, messages, files, users, _, _ := newTestStoresWithRobot(t)

	ticData := buildTICBytes("Area FSX_FILES", "File readme.zip", "Desc test file", "Pw secret1")
	payload := "hello file-echo"

	addr, done := runFakeUplink(t, binkp.Config{
		OurAddresses: []string{"21:3/194"},
		OutboundFiles: []binkp.OutboundFile{
			{Name: "00001122.tic", Size: int64(len(ticData)), ModTime: time.Now(), Data: bytes.NewReader(ticData)},
			{Name: "readme.zip", Size: int64(len(payload)), ModTime: time.Now(), Data: strings.NewReader(payload)},
		},
	})

	res, err := Poll(context.Background(), []string{"21:3/194.1"}, "Test BBS", config.BinkpUplink{
		Address:     "21:3/194",
		Host:        addr,
		TICPassword: "secret1",
	}, nil, netmailStore, messages, users, nil, &TICConfig{Files: files}, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if out := <-done; out.err != nil {
		t.Fatalf("fake uplink answerer error: %v", out.err)
	}
	if res.ReceivedFiles != 1 {
		t.Fatalf("Result.ReceivedFiles = %d, want 1", res.ReceivedFiles)
	}

	area, err := files.AreaByTag("FSX_FILES")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	stored, err := files.ListFiles(area.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(stored) != 1 || stored[0].Filename != "readme.zip" {
		t.Fatalf("stored files = %+v, want exactly one named readme.zip", stored)
	}
}

// TestTICReplacesDeletesTheOlderFiles replays fsxNet's daily
// apodNNNN.zip, whose TIC says "Replaces apod0929.zip", plus a
// wildcard pattern: the older files go, unrelated ones stay, and the
// pattern is kept for forwarding.
func TestTICReplacesDeletesTheOlderFiles(t *testing.T) {
	_, _, files, _, _, _ := newTestStoresWithRobot(t)
	toss := func(name, replaces string) *Result {
		ts := newTICSession(files, nil)
		res := &Result{}
		lines := []string{"Area FSX_ART", "File " + name}
		if replaces != "" {
			lines = append(lines, "Replaces "+replaces)
		}
		if err := ts.receive(name+".tic", bytes.NewReader(buildTICBytes(lines...)), res); err != nil {
			t.Fatal(err)
		}
		if err := ts.receive(name, strings.NewReader("data "+name), res); err != nil {
			t.Fatal(err)
		}
		return res
	}
	toss("apod0929.zip", "apod0928.zip")
	toss("nodelist.271", "")
	toss("readme.txt", "")
	if res := toss("apod0930.zip", "APOD0929.ZIP"); res.ReplacedFiles != 1 {
		t.Fatalf("ReplacedFiles = %d, want 1", res.ReplacedFiles)
	}
	if res := toss("nodelist.272", "NODELIST.*"); res.ReplacedFiles != 1 {
		t.Fatalf("wildcard: ReplacedFiles = %d, want 1", res.ReplacedFiles)
	}

	area, _ := files.AreaByTag("FSX_ART")
	stored, _ := files.ListFiles(area.ID)
	var names []string
	for _, f := range stored {
		names = append(names, f.Filename)
		if f.Filename == "apod0930.zip" && (len(f.Replaces) != 1 || f.Replaces[0] != "APOD0929.ZIP") {
			t.Fatalf("Replaces kept = %q", f.Replaces)
		}
	}
	if got := strings.Join(names, " "); got != "readme.txt apod0930.zip nodelist.272" {
		t.Fatalf("files left = %q", got)
	}
	pf := PendingFileForward{File: stored[1], AreaTag: "FSX_ART"}
	if tic := string(encodeTIC(pf, mail.Address{Zone: 21, Net: 3, Node: 194}, "", 4, 0)); !strings.Contains(tic, "Replaces APOD0929.ZIP\r\n") {
		t.Fatalf("forwarded TIC lacks Replaces:\n%s", tic)
	}
}
