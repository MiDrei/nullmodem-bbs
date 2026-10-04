package push

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestLoadOrCreateKeysKeepsThePair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid.json")
	a, err := LoadOrCreateKeys(path)
	if err != nil || a.Public == "" {
		t.Fatalf("%+v %v", a, err)
	}
	b, err := LoadOrCreateKeys(path)
	if err != nil || b != a {
		t.Fatalf("second load %+v %v, want %+v", b, err, a)
	}
}

func TestNotifierAnnouncesNewMailToTheRecipientsDevices(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	users, messages, nm := user.NewStore(sqlDB), message.NewStore(sqlDB), netmail.NewStore(sqlDB)
	maik, _ := users.Register("SwissMaik", "password123", 100)
	users.SetRealName(maik.ID, "Mike Dreier")
	other, _ := users.Register("other", "password123", 10)
	gen, _ := messages.CreateArea("FSX_GEN", "General", "", "fsxNet", 0, 0)
	dat, _ := messages.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	messages.SetAreaHidden(dat.ID, true)
	sysop, _ := messages.CreateArea("SYSOPS", "Sysops", "", "", 90, 90)

	store := NewStore(sqlDB)
	var sent []string
	sender := &Sender{Store: store, send: func(_ context.Context, msg []byte, s *webpush.Subscription, _ *webpush.Options) (int, error) {
		var n Notification
		json.Unmarshal(msg, &n)
		sent = append(sent, s.Endpoint+" "+n.Title+": "+n.Body)
		if s.Endpoint == "gone" {
			return 410, nil
		}
		return 201, nil
	}}
	store.Save(Subscription{UserID: maik.ID, Endpoint: "phone", P256dh: "k", Auth: "a", Netmail: true, Echomail: true})
	store.Save(Subscription{UserID: maik.ID, Endpoint: "ipad", P256dh: "k", Auth: "a", Netmail: true})
	store.Save(Subscription{UserID: other.ID, Endpoint: "gone", P256dh: "k", Auth: "a", Netmail: true, Echomail: true})

	recv := func(a *message.Area, to, subj string) {
		m, _, err := messages.ReceiveEcho(a.ID, "Someone", subj, "b", "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		sqlDB.Exec(`UPDATE messages SET to_name = ? WHERE id = ?`, to, m.ID)
	}
	// Mail from before the first check isn't announced.
	nm.Receive("Old", "1:2/3", maik.ID, "SwissMaik", "", "old", "b", time.Now(), false)
	// The language comes from the database, as in cmd/web -- which has
	// one connection: looking it up while reading the new mail froze
	// the whole web service (v0.65).
	users.SetLanguage(other.ID, "de")
	n := &Notifier{DB: sqlDB, Sender: sender, Lang: func(id int64) string {
		if u, err := users.ByID(id); err == nil && u.Language != "" {
			return u.Language
		}
		return "en"
	}}
	check := func() error {
		done := make(chan error, 1)
		go func() { done <- n.Check(context.Background()) }()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("Check hangs (the database's one connection is held)")
			return nil
		}
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("first check sent %v", sent)
	}

	nm.Receive("Dean", "21:1/100", maik.ID, "SwissMaik", "", "hello", "b", time.Now(), false)
	nm.Send(maik.ID, "", maik.ID, "SwissMaik", "", "note to self", "b", false)
	nm.Receive("Dean", "21:1/100", other.ID, "other", "", "for other", "b", time.Now(), false)
	recv(gen, "mike dreier", "real name")
	recv(gen, "All", "to all")
	recv(dat, "SwissMaik", "data area")
	recv(sysop, "other", "can't read it")
	own, _ := messages.PostMessage(gen.ID, maik.ID, "SwissMaik", "own post", "b")
	_ = own
	if err := check(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(sent)
	want := []string{
		"gone Netmail von Dean: for other",
		"ipad Netmail from Dean: hello",
		"phone Netmail from Dean: hello",
		"phone Someone in FSX_GEN: real name",
	}
	if len(sent) != len(want) {
		t.Fatalf("sent %q\nwant %q", sent, want)
	}
	for i := range want {
		if sent[i] != want[i] {
			t.Fatalf("sent %q\nwant %q", sent, want)
		}
	}
	if subs, _ := store.ForUser(other.ID); len(subs) != 0 {
		t.Errorf("a subscription the push service answered 410 for is kept: %+v", subs)
	}
	sent = nil
	if err := n.Check(context.Background()); err != nil || len(sent) != 0 {
		t.Fatalf("third check: %v, sent %v", err, sent)
	}
}
