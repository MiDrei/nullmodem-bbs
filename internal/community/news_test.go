package community

import (
	"testing"
	"time"
)

func TestNewsSeenExpiryAndLanguages(t *testing.T) {
	s := testStore(t)
	if _, err := s.SaveNews(News{TitleEN: "Only a title"}); err != ErrNewsEmpty {
		t.Fatalf("news without text: %v", err)
	}
	a, err := s.SaveNews(News{Author: "maik", TitleEN: "Hello", TextEN: "World", TitleDE: "Hallo", TextDE: "Welt"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.SaveNews(News{TitleDE: "Nur Deutsch", TextDE: "Text"})
	old, _ := s.SaveNews(News{TitleEN: "Old", TextEN: "Gone", ExpiresAt: time.Now().Add(-time.Hour)})

	now := time.Now()
	unseen, _ := s.UnseenNews(1, now)
	if len(unseen) != 2 || unseen[0].ID != a || unseen[1].ID != b {
		t.Fatalf("unseen %+v", unseen)
	}
	if title, text := unseen[0].In("de-du"); title != "Hallo" || text != "Welt" {
		t.Errorf("de-du: %q %q", title, text)
	}
	if title, _ := unseen[1].In("en"); title != "Nur Deutsch" {
		t.Errorf("no English: %q", title)
	}
	s.MarkNewsSeen(1, a, b, a)
	if unseen, _ := s.UnseenNews(1, now); len(unseen) != 0 {
		t.Fatalf("still unseen: %+v", unseen)
	}
	if unseen, _ := s.UnseenNews(2, now); len(unseen) != 2 {
		t.Fatalf("bob's: %+v", unseen)
	}
	if all, _ := s.AllNews(); len(all) != 3 {
		t.Fatalf("all: %d", len(all))
	}
	if active, _ := s.ActiveNews(now, 1); len(active) != 1 || active[0].ID == old {
		t.Fatalf("active: %+v", active)
	}

	// Changing keeps who has seen it; deleting removes it.
	if _, err := s.SaveNews(News{ID: a, TitleEN: "Hello!", TextEN: "World"}); err != nil {
		t.Fatal(err)
	}
	if seen, _ := s.SeenNews(1); !seen[a] {
		t.Error("edit forgot who saw it")
	}
	if err := s.DeleteNews(a); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNews(a); err != ErrNotFound {
		t.Fatalf("second delete: %v", err)
	}
}
