package db

import (
	"database/sql"
	"fmt"
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

func TestGuessParentLatestAndAddressee(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	add := func(from, to, subj, at string) int64 {
		t.Helper()
		res, err := d.Exec(`INSERT INTO messages (area_id, from_name, to_name, subject, body, posted_at) VALUES (1, ?, ?, ?, '', ?)`, from, to, subj, at)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	add("Ann", "All", "test", "2026-06-01 10:00:00")            // months before: out of reach
	newTest := add("Bob", "All", "Test", "2026-10-01 10:00:00") // a new "test" thread
	carl := add("Carl", "Bob", "Re: test", "2026-10-01 11:00:00")
	dave := add("Dave", "Bob", "Re: test", "2026-10-01 12:00:00")
	toCarl := add("Bob", "Carl", "Re: test", "2026-10-01 13:00:00")
	toNoOne := add("Eve", "All", "Re: Test", "2026-10-01 14:00:00")
	for _, c := range []struct {
		id, want int64
		to       string
	}{
		{carl, newTest, "Bob"},   // the latest by Bob
		{dave, newTest, "Bob"},   // still Bob's, though Carl's came later
		{toCarl, carl, "Carl"},   // Carl's, not Dave's later one
		{toNoOne, toCarl, "All"}, // no addressee: simply the latest
	} {
		got, err := GuessParent(d, 1, c.id, "Re: test", c.to)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("message %d to %s: parent %d, want %d", c.id, c.to, got, c.want)
		}
	}
}

func TestReguessThreadsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.sqlite")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := d.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	old := exec(`INSERT INTO messages (area_id, from_name, to_name, subject, body, posted_at) VALUES (1, 'Ann', 'All', 'test', '', '2026-03-01 10:00:00')`)
	recent := exec(`INSERT INTO messages (area_id, from_name, to_name, subject, body, posted_at) VALUES (1, 'Bob', 'All', 'test', '', '2026-10-01 10:00:00')`)
	// Linked the old way: to the first "test", half a year back.
	reply := exec(`INSERT INTO messages (area_id, from_name, to_name, subject, body, posted_at, reply_to, reply_guess) VALUES (1, 'Carl', 'All', 'Re: test', '', '2026-10-01 11:00:00', ?, 1)`, old)
	// Our post, sent before out_msgid was kept, and a REPLY to it.
	exec(`INSERT INTO users (id, username, password_hash) VALUES (7, 'maik', 'x')`)
	ours := exec(`INSERT INTO messages (area_id, from_user_id, to_name, subject, body, posted_at) VALUES (1, 7, 'All', 'Modems', '', '2026-10-02 10:00:00')`)
	ourID := fmt.Sprintf("21:3/194 %08x", ours)
	answer := exec(`INSERT INTO messages (area_id, from_name, to_name, subject, body, posted_at, reply_msgid) VALUES (1, 'Dave', 'maik', 'Re: Modems', '', '2026-10-02 11:00:00', ?)`, ourID)
	// A link by REPLY kludge stays as it is.
	kept := exec(`INSERT INTO messages (area_id, from_name, to_name, subject, body, posted_at, reply_to, reply_msgid) VALUES (1, 'Eve', 'All', 'Re: test', '', '2026-10-01 12:00:00', ?, 'x 1')`, old)
	d.Exec(`DELETE FROM meta WHERE key = 'threads_reguessed'`)
	d.Close()
	if d, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	link := func(id int64) (int64, bool) {
		var to sql.NullInt64
		var guess bool
		if err := d.QueryRow(`SELECT reply_to, reply_guess FROM messages WHERE id = ?`, id).Scan(&to, &guess); err != nil {
			t.Fatal(err)
		}
		return to.Int64, guess
	}
	if to, guess := link(reply); to != recent || !guess {
		t.Errorf("reguessed reply: %d (guess %v), want %d", to, guess, recent)
	}
	if to, guess := link(answer); to != ours || guess {
		t.Errorf("answer to our post: %d (guess %v), want %d", to, guess, ours)
	}
	var out string
	d.QueryRow(`SELECT out_msgid FROM messages WHERE id = ?`, ours).Scan(&out)
	if out != ourID {
		t.Errorf("out_msgid = %q", out)
	}
	if to, guess := link(kept); to != old || guess {
		t.Errorf("REPLY link changed: %d (guess %v)", to, guess)
	}
}
