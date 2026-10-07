package message

import (
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestNetNodeFormatsZoneAndPointDropped(t *testing.T) {
	if got := NetNode(3, 100); got != "3/100" {
		t.Fatalf("NetNode(3, 100) = %q, want %q", got, "3/100")
	}
}

func TestSeenByNetNodesFindsEveryEntryAcrossMultipleLines(t *testing.T) {
	body := "Hello there\r\n--- tearline\r\n* Origin: Test\r\nSEEN-BY: 3/100 3/101\r\nSEEN-BY: 954/700\r\n\x01PATH: 3/100\r\n"
	got := SeenByNetNodes(body)
	want := map[string]bool{"3/100": true, "3/101": true, "954/700": true}
	if len(got) != len(want) {
		t.Fatalf("SeenByNetNodes = %v, want %v", got, want)
	}
	for nn := range want {
		if !got[nn] {
			t.Fatalf("SeenByNetNodes = %v, missing %q", got, nn)
		}
	}
}

func TestSeenByNetNodesIsCaseInsensitiveAndIgnoresJunkTokens(t *testing.T) {
	body := "seen-by: 3/100 notanaddress 3/101\r\n"
	got := SeenByNetNodes(body)
	if len(got) != 2 || !got["3/100"] || !got["3/101"] {
		t.Fatalf("SeenByNetNodes = %v, want just 3/100 and 3/101", got)
	}
}

func TestSeenByNetNodesEmptyBodyReturnsEmptyMap(t *testing.T) {
	got := SeenByNetNodes("no seen-by lines here")
	if len(got) != 0 {
		t.Fatalf("SeenByNetNodes = %v, want empty", got)
	}
}

func TestAddSeenByLineAppendsOnlyNewEntries(t *testing.T) {
	body := "Hello\r\nSEEN-BY: 3/100\r\n"
	updated := addSeenByLine(body, []string{"3/100", "3/101", "954/700"})
	got := SeenByNetNodes(updated)
	if len(got) != 3 || !got["3/100"] || !got["3/101"] || !got["954/700"] {
		t.Fatalf("SeenByNetNodes(updated) = %v, want all three entries present", got)
	}
	if !strings.Contains(updated, "SEEN-BY: 3/101 954/700") {
		t.Fatalf("updated body = %q, want a new SEEN-BY line with just the new entries (sorted)", updated)
	}
}

func TestAddSeenByLineNoOpWhenAllAlreadyPresent(t *testing.T) {
	body := "Hello\r\nSEEN-BY: 3/100 3/101\r\n"
	updated := addSeenByLine(body, []string{"3/100", "3/101"})
	if updated != body {
		t.Fatalf("addSeenByLine = %q, want unchanged %q when every entry is already listed", updated, body)
	}
}

func TestMarkSeenByAddsEntryToStoredBody(t *testing.T) {
	s, users := newTestStore(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	posted, err := s.PostMessage(area.ID, u.ID, "All", "Hi", "hello world")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if err := s.MarkSeenBy(posted.ID, "3/100"); err != nil {
		t.Fatalf("MarkSeenBy: %v", err)
	}

	reloaded, err := s.MessageByID(posted.ID)
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	if !SeenByNetNodes(reloaded.Body)["3/100"] {
		t.Fatalf("reloaded body = %q, want it to now list 3/100 in SEEN-BY", reloaded.Body)
	}
}

func TestMarkSeenByIsIdempotent(t *testing.T) {
	s, users := newTestStore(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	posted, err := s.PostMessage(area.ID, u.ID, "All", "Hi", "hello world")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if err := s.MarkSeenBy(posted.ID, "3/100"); err != nil {
		t.Fatalf("first MarkSeenBy: %v", err)
	}
	if err := s.MarkSeenBy(posted.ID, "3/100"); err != nil {
		t.Fatalf("second MarkSeenBy: %v", err)
	}

	reloaded, err := s.MessageByID(posted.ID)
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	count := 0
	for _, line := range strings.Split(reloaded.Body, "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "SEEN-BY:") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("body has %d SEEN-BY line(s) after marking the same net/node twice, want exactly 1: %q", count, reloaded.Body)
	}
}
