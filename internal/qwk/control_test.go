package qwk

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriteControlDATLineOrder(t *testing.T) {
	c := ControlInfo{
		BBSName:    "Maiks Place BBS",
		City:       "Zurich, CH",
		Phone:      "000-000-0000",
		SysopName:  "SwissMaik",
		BBSID:      "nullmodm",
		PacketTime: time.Date(2026, time.September, 23, 14, 5, 30, 0, time.UTC),
		CallerName: "alice",
		Conferences: []ConferenceInfo{
			{Number: 0, Name: "Personal"},
			{Number: 3, Name: "General Chat"},
		},
	}
	var buf bytes.Buffer
	if err := WriteControlDAT(&buf, c); err != nil {
		t.Fatalf("WriteControlDAT: %v", err)
	}
	lines := strings.Split(buf.String(), "\r\n")

	want := []string{
		"Maiks Place BBS",
		"Zurich, CH",
		"000-000-0000",
		"SwissMaik,Sysop",
		"00000,NULLMODM",
		"09-23-2026,14:05:30",
		"ALICE",
		"",
		"0",
		"0",
		"1", // len(Conferences)-1 = 2-1
		"0",
		"Personal",
		"3",
		"General Chat",
		"WELCOME",
		"NEWS",
		"GOODBYE",
	}
	if len(lines) < len(want) {
		t.Fatalf("got %d lines, want at least %d: %q", len(lines), len(want), lines)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}

func TestWriteControlDATTruncatesLongConferenceNames(t *testing.T) {
	c := ControlInfo{
		BBSID:      "id",
		PacketTime: time.Now(),
		Conferences: []ConferenceInfo{
			{Number: 0, Name: "This Name Is Way Too Long For QWK"},
		},
	}
	var buf bytes.Buffer
	if err := WriteControlDAT(&buf, c); err != nil {
		t.Fatalf("WriteControlDAT: %v", err)
	}
	lines := strings.Split(buf.String(), "\r\n")
	// line index 12 is the conference-0 name (11 header lines, index 0-10, then
	// conf number at 11, name at 12).
	if len(lines[12]) != 12 {
		t.Fatalf("conference name line = %q (len %d), want truncated to 12 chars", lines[12], len(lines[12]))
	}
}
