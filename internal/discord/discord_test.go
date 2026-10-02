package discord

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
)

func TestOutgoing(t *testing.T) {
	set := Settings{BBSName: "Maiks Place"}
	for _, c := range []struct {
		l          chat.Line
		quiet      bool
		name, text string
		ok         bool
	}{
		{chat.Line{Username: "maik", Source: "node 1", Kind: chat.Say, Text: "hallo"}, false, "maik", "hallo", true},
		{chat.Line{Username: "Bob", Source: chat.SourceDiscord, Kind: chat.Say, Text: "hi"}, false, "", "", false},
		{chat.Line{Username: "maik", Source: "node 1", Kind: chat.Join}, false, "Maiks Place", "*maik joined on the BBS*", true},
		{chat.Line{Username: "maik", Source: "web", Kind: chat.Join}, false, "Maiks Place", "*maik joined on the web*", true},
		{chat.Line{Username: "maik", Source: "node 1", Kind: chat.Leave}, false, "Maiks Place", "*maik left*", true},
		{chat.Line{Username: "maik", Source: "node 1", Kind: chat.Join}, true, "", "", false},
		{chat.Line{Username: "maik", Source: "node 1", Kind: chat.Page, Text: "help"}, false, "", "", false},
	} {
		set.Quiet = c.quiet
		name, text, ok := outgoing(c.l, set)
		if name != c.name || text != c.text || ok != c.ok {
			t.Errorf("%+v -> %q %q %v", c.l, name, text, ok)
		}
	}
}

func TestIncoming(t *testing.T) {
	got := incoming("hey <:pepe:123456> look\n\n  second  \n", []string{"https://cdn.example/x.png"})
	want := []string{"hey :pepe: look", "second", "https://cdn.example/x.png"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("incoming = %q", got)
	}
	long := incoming(strings.Repeat("line\n", 10), nil)
	if len(long) != MaxLines || !strings.Contains(long[MaxLines-1], "5 more lines") {
		t.Errorf("long = %q", long)
	}
}

func TestNames(t *testing.T) {
	if n := webhookUsername("Discord@Fan#1"); strings.Contains(strings.ToLower(n), "discord") || strings.ContainsAny(n, "@#") {
		t.Errorf("webhook name %q", n)
	}
	if n := displayName("bob99", "Bob", "Bobby"); n != "Bobby" {
		t.Errorf("display %q", n)
	}
	if n := displayName("bob99", "", ""); n != "bob99" {
		t.Errorf("display %q", n)
	}
}

type quiet struct{}

func (quiet) Info(string, ...any) {}
func (quiet) Warn(string, ...any) {}

// Disabled, the bridge connects nowhere and lets lines pass by; the
// rooms' channels are known for messages coming in.
func TestBridgeDisabledSkipsLines(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := chat.NewStore(sqlDB)
	store.SaveRoom(chat.RoomInfo{Name: "tech", Title: "Tech", DiscordChannel: "42"})
	b := &Bridge{Chat: store, Logger: quiet{}, Every: 10 * time.Millisecond,
		Settings: func() Settings { return Settings{DiscordConfig: config.DiscordConfig{Enabled: false, Token: "x"}} }}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	go store.Post("tech", "maik", "node 1", chat.Say, "hi")
	b.Run(ctx)
	st := b.Status()
	if st.Connected || st.Enabled || !st.HasToken {
		t.Errorf("status %+v", st)
	}
	last, _ := store.LastLineID()
	if b.lastID != last {
		t.Errorf("lastID %d, want %d", b.lastID, last)
	}
	if b.channels["42"] != "tech" {
		t.Errorf("channels %v", b.channels)
	}
	if down, _ := b.Down(0); down {
		t.Error("a disabled bridge isn't down")
	}
}
