package applog

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewStore(sqlDB)
}

func TestLoggerWritesAndRecentReturnsChronological(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, "bbs")

	logger.Info("first")
	logger.Warn("second")
	logger.Error("third")

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("Recent returned %d entries, want 3", len(entries))
	}
	if entries[0].Message != "first" || entries[1].Message != "second" || entries[2].Message != "third" {
		t.Fatalf("Recent order = %+v, want chronological first/second/third", entries)
	}
	if entries[0].Level != LevelInfo || entries[1].Level != LevelWarn || entries[2].Level != LevelError {
		t.Fatalf("levels = %+v, want info/warn/error", entries)
	}
	for _, e := range entries {
		if e.Source != "bbs" {
			t.Fatalf("Source = %q, want %q", e.Source, "bbs")
		}
	}
}

func TestRecentRespectsLimit(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, "bbs")
	for i := 0; i < 5; i++ {
		logger.Info("line %d", i)
	}

	entries, err := store.Recent(2)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Recent(2) returned %d entries, want 2", len(entries))
	}
	// Most recent two, still chronological: "line 3" then "line 4".
	if entries[0].Message != "line 3" || entries[1].Message != "line 4" {
		t.Fatalf("Recent(2) = %+v, want the last two entries in order", entries)
	}
}

func TestSinceReturnsOnlyNewerEntries(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, "web")

	logger.Info("a")
	logger.Info("b")
	first, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	lastID := first[len(first)-1].ID

	logger.Info("c")
	logger.Info("d")

	newEntries, err := store.Since(lastID, 10)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(newEntries) != 2 || newEntries[0].Message != "c" || newEntries[1].Message != "d" {
		t.Fatalf("Since(lastID) = %+v, want just c and d", newEntries)
	}
}

func TestFormattingArgsAreApplied(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, "bbs")

	logger.Info("node %d connected from %s", 3, "127.0.0.1")

	entries, err := store.Recent(1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	want := "node 3 connected from 127.0.0.1"
	if len(entries) != 1 || entries[0].Message != want {
		t.Fatalf("entries = %+v, want message %q", entries, want)
	}
}

func TestTrimDoesNotFireBelowMaxRows(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, "bbs")

	for i := 0; i < trimEvery+5; i++ {
		logger.Info("line %d", i)
	}

	entries, err := store.Recent(maxRows + 1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != trimEvery+5 {
		t.Fatalf("entry count = %d, want %d (trim should not remove anything below maxRows)", len(entries), trimEvery+5)
	}
}

func TestTrimActuallyPrunesOldRows(t *testing.T) {
	// Temporarily shrink the thresholds so this test can exercise real
	// trimming without inserting thousands of rows.
	origTrimEvery, origMaxRows := trimEvery, maxRows
	trimEvery, maxRows = 5, 10
	t.Cleanup(func() { trimEvery, maxRows = origTrimEvery, origMaxRows })

	store := newTestStore(t)
	logger := NewLogger(store, "bbs")

	for i := 0; i < 23; i++ {
		logger.Info("line %d", i)
	}

	entries, err := store.Recent(1000)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	// Trimming happens periodically (every trimEvery inserts), not on
	// every single insert, so the table can transiently hold up to
	// trimEvery-1 rows more than maxRows between trims -- it's bounded,
	// not held exactly at maxRows at all times.
	maxTransient := maxRows + trimEvery - 1
	if len(entries) > maxTransient {
		t.Fatalf("entry count = %d, want at most %d (maxRows + trimEvery - 1)", len(entries), maxTransient)
	}
	// The oldest surviving entry must be a later one, not "line 0" --
	// confirms trim actually deleted old rows rather than being a
	// no-op.
	if entries[0].Message == "line 0" {
		t.Fatalf("oldest surviving entry is %q, want old entries to have been trimmed away", entries[0].Message)
	}
	// The most recent entry must always survive.
	if entries[len(entries)-1].Message != "line 22" {
		t.Fatalf("newest entry = %q, want %q", entries[len(entries)-1].Message, "line 22")
	}
}

func TestListFiltersByCategoryLevelPrefixAndText(t *testing.T) {
	store := newTestStore(t)
	add := func(source string, level Level, msg string) {
		if err := store.insert(source, level, msg); err != nil {
			t.Fatal(err)
		}
	}
	add("bbs", LevelInfo, "[telnet] maik logged in")
	add("bbs", LevelInfo, "[ssh] bob logged in")
	add("bbs", LevelInfo, "bbs daemon starting")
	add("bbs", LevelInfo, "MRC Chat: Ready for clients.")
	add("bbs", LevelWarn, "MRC Chat: background program exited")
	add("mailer", LevelWarn, "polling 23:1/1: connection refused")
	add("mailer", LevelInfo, "polled 21:3/100")
	add("web", LevelError, "boom")

	msgs := func(f Filter) []string {
		t.Helper()
		es, err := store.List(f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range es {
			out = append(out, e.Message)
		}
		return out
	}
	check := func(name string, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	check("telnet", msgs(Filter{Category: CategoryTelnet}), "[telnet] maik logged in")
	check("system", msgs(Filter{Category: CategorySystem}),
		"bbs daemon starting", "MRC Chat: Ready for clients.", "MRC Chat: background program exited")
	check("system, one door", msgs(Filter{Category: CategorySystem, Prefixes: []string{"MRC Chat:"}}),
		"MRC Chat: Ready for clients.", "MRC Chat: background program exited")
	check("system, no doors", msgs(Filter{Category: CategorySystem, ExcludePrefixes: []string{"MRC Chat:"}}),
		"bbs daemon starting")
	check("problems everywhere", msgs(Filter{Levels: []Level{LevelWarn, LevelError}}),
		"MRC Chat: background program exited", "polling 23:1/1: connection refused", "boom")
	check("text", msgs(Filter{Category: CategoryMailer, Query: "REFUSED"}), "polling 23:1/1: connection refused")
	check("newest 2, oldest first", msgs(Filter{Limit: 2}), "polled 21:3/100", "boom")

	all, _ := store.List(Filter{})
	check("before", msgs(Filter{BeforeID: all[2].ID, Limit: 1}), "[ssh] bob logged in")
	check("after", msgs(Filter{AfterID: all[5].ID}), "polled 21:3/100", "boom")
	if _, err := store.List(Filter{Category: "nope"}); err != ErrUnknownCategory {
		t.Errorf("unknown category: %v", err)
	}
}
