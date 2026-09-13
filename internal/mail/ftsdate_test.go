package mail

import (
	"strings"
	"testing"
	"time"
)

func TestFormatFTSCDate(t *testing.T) {
	got := formatFTSCDate(time.Date(2026, time.January, 3, 23, 51, 10, 0, time.UTC))
	want := "03 Jan 26  23:51:10"
	if got != want {
		t.Errorf("formatFTSCDate() = %q, want %q", got, want)
	}
}

func TestParseFTSCDate(t *testing.T) {
	got, err := parseFTSCDate("03 Jan 26  23:51:10\x00")
	if err != nil {
		t.Fatalf("parseFTSCDate: %v", err)
	}
	want := time.Date(2026, time.January, 3, 23, 51, 10, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parseFTSCDate() = %v, want %v", got, want)
	}
}

func TestParseFTSCDateYearPivot(t *testing.T) {
	got, err := parseFTSCDate("01 Jan 69  00:00:00")
	if err != nil {
		t.Fatalf("parseFTSCDate: %v", err)
	}
	if got.Year() != 2069 {
		t.Errorf("year = %d, want 2069", got.Year())
	}

	got, err = parseFTSCDate("01 Jan 70  00:00:00")
	if err != nil {
		t.Fatalf("parseFTSCDate: %v", err)
	}
	if got.Year() != 1970 {
		t.Errorf("year = %d, want 1970", got.Year())
	}

	got, err = parseFTSCDate("01 Jan 99  00:00:00")
	if err != nil {
		t.Fatalf("parseFTSCDate: %v", err)
	}
	if got.Year() != 1999 {
		t.Errorf("year = %d, want 1999", got.Year())
	}
}

func TestParseFTSCDateErrors(t *testing.T) {
	cases := []string{
		"",
		"garbage",
		"03 Foo 26  23:51:10",
		"aa Jan 26  23:51:10",
		"03 Jan 26  23:51",
		"03 Jan 26  aa:51:10",
	}
	for _, in := range cases {
		if _, err := parseFTSCDate(in); err == nil {
			t.Errorf("parseFTSCDate(%q): want error, got nil", in)
		}
	}
}

func TestFTSCDateRoundTrip(t *testing.T) {
	tm := time.Date(2026, time.September, 13, 12, 30, 45, 0, time.UTC)
	s := formatFTSCDate(tm)
	if !strings.Contains(s, "  ") {
		t.Errorf("formatted date %q missing double space before time", s)
	}
	got, err := parseFTSCDate(s)
	if err != nil {
		t.Fatalf("parseFTSCDate(%q): %v", s, err)
	}
	if !got.Equal(tm) {
		t.Errorf("round trip = %v, want %v", got, tm)
	}
}
