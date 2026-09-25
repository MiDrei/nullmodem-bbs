package areafix

import (
	"strings"
	"testing"
)

// TestParseAreaListReplyBasicFormat deliberately omits a "Area Tag
// / Description"-style header row: a heuristic tag-shaped-token parser
// with no fixed schema to check against can't reliably distinguish a
// header from a real entry (both look like "word word..."), so a
// header is a known, accepted false positive left for the sysop to
// spot via the raw reply text shown alongside the parsed list --
// hence testing only what the parser DOES promise: real area lines.
func TestParseAreaListReplyBasicFormat(t *testing.T) {
	body := "----------------  -----------------------------\r\n" +
		" FSX_GEN           fsxNet General Discussion\r\n" +
		"+FSX_ADS           fsxNet BBS Advertisements\r\n" +
		"*FSX_TEST          fsxNet Testing Area\r\n" +
		"\r\n" +
		"3 areas listed.\r\n" +
		"\r\n" +
		"--- Mystic BBS v1.12 A48 (Linux/64)\r\n" +
		"* Origin: Some Hub (21:3/100)\r\n" +
		"SEEN-BY: 3/100\r\n" +
		"\x01PATH: 3/100\r\n"

	got := ParseAreaListReply(body)
	want := []ParsedArea{
		{Tag: "FSX_GEN", Description: "fsxNet General Discussion", Subscribed: false},
		{Tag: "FSX_ADS", Description: "fsxNet BBS Advertisements", Subscribed: true},
		{Tag: "FSX_TEST", Description: "fsxNet Testing Area", Subscribed: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestParseAreaListReplyColonTableFormat is a regression test using a
// real "%LIST" reply captured live from an actual hub ("Clearing
// Houz"): a bordered ':'-delimited table, with the area's description
// itself containing a ':' ("FSX: Ads...") and a trailing numeric
// message-count column -- the original whitespace-only parser missed
// every single entry in this format.
func TestParseAreaListReplyColonTableFormat(t *testing.T) {
	body := "TZUTC: 1000\n\x01MSGID: 21:3/100.0 a61329a6\n\x01PID: clrghouz 4e9cfb05\n" +
		"\x01INTL 21:3/194 21:3/100\n" +
		" Here are the list of available echoareas:\n\n" +
		":---:------------:--------------------------------------------------:------:\n" +
		":   : AREA       : DESCRIPTION                                      : MSGS :\n" +
		":---:------------:--------------------------------------------------:------:\n" +
		":*  : FSX_ADS    : FSX: Ads + ANSI Art                              :  551 :\n" +
		":*  : FSX_GEN    : FSX: General Chat + More..                       :  182 :\n" +
		":+  : FSX_HAM    : FSX: HAM + Radio Chat                            :    0 :\n" +
		":---:------------:--------------------------------------------------:------:\n" +
		"\n'*' = Subscribed, '+' = available, 'R' = read only, 'W' = write only\n" +
		"(MSGS = Messages in the last month)\n\n" +
		"--- Clearing Houz (AB8D)\n"

	got := ParseAreaListReply(body)
	want := []ParsedArea{
		{Tag: "FSX_ADS", Description: "FSX: Ads + ANSI Art", Subscribed: true},
		{Tag: "FSX_GEN", Description: "FSX: General Chat + More..", Subscribed: true},
		{Tag: "FSX_HAM", Description: "FSX: HAM + Radio Chat", Subscribed: false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestParseAreaListReplyIgnoresWrappedContinuationLines is a
// regression test using a real "%LIST" reply captured live from
// another hub: with long descriptions, a table row wraps onto a
// second physical line, and that continuation line's first word
// ("patterns.", ending in ordinary sentence punctuation) previously
// passed the area-tag shape check and was parsed as a bogus area of
// its own.
func TestParseAreaListReplyIgnoresWrappedContinuationLines(t *testing.T) {
	body := ":---:------------:--------------------------------------------------:------:\n" +
		":   : AREA       : DESCRIPTION                                      : MSGS :\n" +
		":---:------------:--------------------------------------------------:------:\n" +
		":*  : HNET_BEADING : HOB: The craft of beading and beadwork jewelry and\n" +
		"patterns. :    2 :\n" +
		":*  : HNET_CHAT  : HOB: Chat about any topic                        :   21 :\n" +
		":---:------------:--------------------------------------------------:------:\n"

	got := ParseAreaListReply(body)
	want := []ParsedArea{
		{Tag: "HNET_BEADING", Description: "HOB: The craft of beading and beadwork jewelry and", Subscribed: true},
		{Tag: "HNET_CHAT", Description: "HOB: Chat about any topic", Subscribed: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestParseAreaListReplyMarkerDotsQuotedFormat is a regression test
// using a real "%LIST" reply captured live from a third hub (a husky/
// hpt-based Areafix robot): flag letters with no space between them,
// dot-fill padding, and a quoted description that sometimes wraps
// onto the next line before its closing quote.
func TestParseAreaListReplyMarkerDotsQuotedFormat(t *testing.T) {
	body := "Available areas for 227:1/23\n\n" +
		"*S   LVLY_ADULT ............... \"Mature/18+ topics of discussion, humour, etc.\"\n" +
		"*S   LVLY_CHAT ............................................ \"General Chit Chat\"\n" +
		"*S   LVLY_COLDWARCOMMS ............................................... \"Coldwar\n" +
		"                            Communications with an emphasis on AT&T Longlines\"\n" +
		"\n'*' = area is active\n'R' = area is readonly for you\n\n" +
		" 3 area(s) available, 3 area(s) linked\n"

	got := ParseAreaListReply(body)
	want := []ParsedArea{
		{Tag: "LVLY_ADULT", Description: "Mature/18+ topics of discussion, humour, etc.", Subscribed: true},
		{Tag: "LVLY_CHAT", Description: "General Chit Chat", Subscribed: true},
		{Tag: "LVLY_COLDWARCOMMS", Description: "Coldwar Communications with an emphasis on AT&T Longlines", Subscribed: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseAreaListReplySkipsDividerAndCountLines(t *testing.T) {
	body := "===============================\r\n" +
		"157\r\n" +
		"\r\n"
	if got := ParseAreaListReply(body); len(got) != 0 {
		t.Fatalf("got %+v, want no entries (divider/bare-number lines aren't areas)", got)
	}
}

func TestParseAreaListReplyEmptyBodyReturnsNil(t *testing.T) {
	if got := ParseAreaListReply(""); len(got) != 0 {
		t.Fatalf("got %+v, want no entries", got)
	}
}

func TestLooksLikeAreaTag(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"FSX_GEN", true},
		{"fsx.general-chat", true},
		{"AB", true},
		{"A", false},                           // too short
		{"157", false},                         // no letter
		{"----", false},                        // no letter, disallowed chars
		{"A" + strings.Repeat("B", 41), false}, // too long
	}
	for _, c := range cases {
		if got := looksLikeAreaTag(c.s); got != c.want {
			t.Errorf("looksLikeAreaTag(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}
