package file

import (
	"strings"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func TestFileNetNodeFormatsZoneAndPointDropped(t *testing.T) {
	if got := NetNode(3, 100); got != "3/100" {
		t.Fatalf("NetNode(3, 100) = %q, want %q", got, "3/100")
	}
}

func TestFileSeenByNetNodesParsesSpaceSeparatedList(t *testing.T) {
	got := SeenByNetNodes("3/100 3/101 954/700")
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

func TestFileSeenByNetNodesEmptyStringReturnsEmptyMap(t *testing.T) {
	if got := SeenByNetNodes(""); len(got) != 0 {
		t.Fatalf("SeenByNetNodes(\"\") = %v, want empty", got)
	}
}

func TestFileSeenByNetNodesIgnoresJunkTokens(t *testing.T) {
	got := SeenByNetNodes("3/100 notanaddress 3/101")
	if len(got) != 2 || !got["3/100"] || !got["3/101"] {
		t.Fatalf("SeenByNetNodes = %v, want just 3/100 and 3/101", got)
	}
}

func TestFileMarkSeenByAddsEntryToStoredColumn(t *testing.T) {
	s, users := newTestStore(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	f, err := s.UploadFile(area.ID, u.ID, "notes.txt", "", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	if err := s.MarkSeenBy(f.ID, "3/100"); err != nil {
		t.Fatalf("MarkSeenBy: %v", err)
	}

	reloaded, err := s.FileByID(f.ID)
	if err != nil {
		t.Fatalf("FileByID: %v", err)
	}
	if !SeenByNetNodes(reloaded.SeenBy)["3/100"] {
		t.Fatalf("reloaded.SeenBy = %q, want it to list 3/100", reloaded.SeenBy)
	}
}

func TestFileMarkSeenByIsIdempotent(t *testing.T) {
	s, users := newTestStore(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	f, err := s.UploadFile(area.ID, u.ID, "notes.txt", "", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	if err := s.MarkSeenBy(f.ID, "3/100"); err != nil {
		t.Fatalf("first MarkSeenBy: %v", err)
	}
	if err := s.MarkSeenBy(f.ID, "3/100"); err != nil {
		t.Fatalf("second MarkSeenBy: %v", err)
	}

	reloaded, err := s.FileByID(f.ID)
	if err != nil {
		t.Fatalf("FileByID: %v", err)
	}
	if len(strings.Fields(reloaded.SeenBy)) != 1 {
		t.Fatalf("reloaded.SeenBy = %q, want exactly one entry after marking the same net/node twice", reloaded.SeenBy)
	}
}

func TestFileMarkSeenByAppendsMultipleDownlinks(t *testing.T) {
	s, users := newTestStore(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	f, err := s.UploadFile(area.ID, u.ID, "notes.txt", "", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	if err := s.MarkSeenBy(f.ID, "3/100"); err != nil {
		t.Fatalf("MarkSeenBy 3/100: %v", err)
	}
	if err := s.MarkSeenBy(f.ID, "954/700"); err != nil {
		t.Fatalf("MarkSeenBy 954/700: %v", err)
	}

	reloaded, err := s.FileByID(f.ID)
	if err != nil {
		t.Fatalf("FileByID: %v", err)
	}
	got := SeenByNetNodes(reloaded.SeenBy)
	if len(got) != 2 || !got["3/100"] || !got["954/700"] {
		t.Fatalf("reloaded.SeenBy = %q, want both 3/100 and 954/700", reloaded.SeenBy)
	}
}
