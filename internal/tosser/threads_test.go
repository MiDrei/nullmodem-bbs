package tosser

import (
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/mail"
)

func TestRepliesThreadBothWays(t *testing.T) {
	netmailStore, messages, users := newTestStores(t)
	maik, _ := users.Register("maik", "password123", 10)
	area, _ := messages.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	hub := func(body string) {
		t.Helper()
		msg := mail.Message{OrigAddr: mustAddr(t, "21:1/1"), DestAddr: mustAddr(t, "21:3/194"), Written: time.Now(), FromName: "Bob", ToName: "All", Subject: "Re: Topic", Body: body}
		if _, err := tossInbound(pointPacket(t, msg), nil, netmailStore, messages, users, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	hub("AREA:FSX_GEN\n\x01MSGID: 21:1/1 11111111\nThe topic.\n")
	list, _ := messages.ListMessages(area.ID)
	topic := list[0]

	// Our reply goes out with REPLY naming the topic's MSGID ...
	mine, _ := messages.PostMessage(area.ID, maik.ID, "Bob", "Re: Topic", "Mine.")
	messages.SetReplyTo(mine.ID, topic.ID)
	up, _ := RoutedOutboundEcho(messages, fsxHub)
	echoAddr := mustAddr(t, "21:3/194")
	if err := threadKludges(messages, echoAddr, up); err != nil {
		t.Fatal(err)
	}
	buf, err := buildPacket(echoAddr, mustAddr(t, "21:3/100"), "", "BBS", nil, up, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x01REPLY: 21:1/1 11111111\r") {
		t.Fatalf("no REPLY kludge in:\n%q", buf.String())
	}

	// ... and an answer to it, naming the MSGID it went out under, joins it.
	hub("AREA:FSX_GEN\n\x01MSGID: 21:1/1 22222222\n\x01REPLY: " + localMsgID(echoAddr, mine.ID) + "\nAnswer.\n")
	th, err := messages.ThreadOf(topic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(th) != 3 || th[1].ID != mine.ID || th[2].ReplyTo != mine.ID || th[2].Depth != 2 {
		t.Fatalf("thread = %+v", th)
	}
}
