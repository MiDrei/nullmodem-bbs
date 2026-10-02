package db

import (
	"path/filepath"
	"testing"
)

func TestBackfillThreadsBySubjectOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.sqlite")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, subj := range []string{"Hello", "Re: Hello", "Other", "RE^2: hello", "Re: Unknown"} {
		if _, err := d.Exec(`INSERT INTO messages (area_id, subject, body, posted_at) VALUES (1, ?, '', datetime('2026-09-01', ? || ' minutes'))`, subj, i); err != nil {
			t.Fatal(err)
		}
	}
	// As if these predate threading.
	d.Exec(`DELETE FROM meta WHERE key = 'threads_backfilled'`)
	d.Close()
	if d, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	got := map[string]any{}
	rows, _ := d.Query(`SELECT subject, reply_to, reply_guess FROM messages ORDER BY id`)
	for rows.Next() {
		var s string
		var r any
		var g int
		rows.Scan(&s, &r, &g)
		got[s] = r
	}
	rows.Close()
	if got["Re: Hello"] != int64(1) || got["RE^2: hello"] != int64(1) || got["Other"] != nil || got["Re: Unknown"] != nil {
		t.Fatalf("links = %v", got)
	}
}
