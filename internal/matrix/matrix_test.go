package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
)

func TestIncoming(t *testing.T) {
	reply := messageContent{MsgType: "m.text", Body: "> <@bob:x> earlier\n> more\n\nmy answer"}
	reply.RelatesTo = &struct {
		InReplyTo *struct {
			EventID string `json:"event_id"`
		} `json:"m.in_reply_to"`
		RelType string `json:"rel_type"`
	}{InReplyTo: &struct {
		EventID string `json:"event_id"`
	}{EventID: "$1"}}
	if got := incoming(reply); len(got) != 1 || got[0] != "my answer" {
		t.Errorf("reply: %q", got)
	}
	if got := incoming(messageContent{MsgType: "m.emote", Body: "waves"}); got[0] != "* waves" {
		t.Errorf("emote: %q", got)
	}
	if got := incoming(messageContent{MsgType: "m.notice", Body: "bot"}); got != nil {
		t.Errorf("notice: %q", got)
	}
	if got := incoming(messageContent{MsgType: "m.text", Body: strings.Repeat("x\n", 10)}); len(got) != MaxLines {
		t.Errorf("long: %d lines", len(got))
	}
}

type quiet struct{}

func (quiet) Info(string, ...any) {}
func (quiet) Warn(string, ...any) {}

// TestBridgeAgainstHomeserver needs a homeserver allowing registration
// with token "testtoken", e.g. a local Conduit:
// MATRIX_TEST_HS=http://127.0.0.1:6167 go test ./internal/matrix
func TestBridgeAgainstHomeserver(t *testing.T) {
	hs := os.Getenv("MATRIX_TEST_HS")
	if hs == "" {
		t.Skip("MATRIX_TEST_HS not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stamp := time.Now().Format("150405")
	register := func(name string) *Client {
		c := &Client{Homeserver: hs}
		// The first step answers 401 with the session to go on with.
		var first struct {
			Session string `json:"session"`
		}
		res, err := http.Post(hs+"/_matrix/client/v3/register", "application/json",
			strings.NewReader(`{"username":"`+name+`","password":"pw-`+name+`-12345"}`))
		if err != nil {
			t.Fatal(err)
		}
		json.NewDecoder(res.Body).Decode(&first)
		res.Body.Close()
		var out struct {
			Token string `json:"access_token"`
		}
		if err := c.do(ctx, http.MethodPost, "/_matrix/client/v3/register", map[string]any{"username": name, "password": "pw-" + name + "-12345",
			"auth": map[string]string{"type": "m.login.registration_token", "token": "testtoken", "session": first.Session}}, &out); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		c.Token = out.Token
		return c
	}
	register("bot" + stamp)
	botID, botToken, err := Login(ctx, hs, "bot"+stamp, "pw-bot"+stamp+"-12345")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	human := register("eve" + stamp)
	var room struct {
		RoomID string `json:"room_id"`
	}
	if err := human.do(ctx, http.MethodPost, "/_matrix/client/v3/createRoom", map[string]any{"name": "BBS chat", "preset": "public_chat"}, &room); err != nil {
		t.Fatal(err)
	}
	human.do(ctx, http.MethodPost, "/_matrix/client/v3/rooms/"+room.RoomID+"/invite", map[string]string{"user_id": botID}, nil)

	sqlDB, _ := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	defer sqlDB.Close()
	store := chat.NewStore(sqlDB)
	store.SaveRoom(chat.RoomInfo{Name: "main", Title: "Teleconference", MatrixRoom: room.RoomID})
	b := &Bridge{Chat: store, Logger: quiet{}, Every: 100 * time.Millisecond, Settings: func() Settings {
		return Settings{MatrixConfig: config.MatrixConfig{Enabled: true, Homeserver: hs, UserID: botID, Token: botToken}, BBSName: "Test BBS"}
	}}
	go b.Run(ctx)
	waitFor := func(what string, ok func() bool) {
		for i := 0; i < 100; i++ {
			if ok() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		lines, _ := store.Lines("main", 0, 20)
		t.Fatalf("waited in vain for %s; status %+v; lines %+v", what, b.Status(), lines)
	}
	waitFor("the bot in the room", func() bool {
		c := b.Client()
		if c == nil || !b.Status().Connected {
			return false
		}
		rooms, _ := c.JoinedRooms(ctx)
		return len(rooms) == 1
	})

	// Matrix -> BBS.
	(&Client{Homeserver: hs, Token: human.Token}).Send(ctx, room.RoomID, "m.text", "hello from matrix", "")
	waitFor("the line in the BBS room", func() bool {
		lines, _ := store.Lines("main", 0, 10)
		for _, l := range lines {
			if l.Source == chat.SourceMatrix && l.Text == "hello from matrix" && strings.HasPrefix(l.Username, "eve"+stamp) {
				return true
			}
		}
		return false
	})

	// BBS -> Matrix.
	store.Post("main", "maik", "node 1", chat.Say, "hello from the BBS")
	var since string
	waitFor("the message in Matrix", func() bool {
		res, err := human.Sync(ctx, since, time.Second)
		if err != nil {
			return false
		}
		since = res.NextBatch
		for _, ev := range res.Rooms.Join[room.RoomID].Timeline.Events {
			if ev.Sender == botID && strings.Contains(string(ev.Content), "maik: hello from the BBS") {
				return true
			}
		}
		return false
	})
}
