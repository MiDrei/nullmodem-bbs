package tosser

import (
	"fmt"

	"git.maik.ch/nullmodem/bbs/internal/mail"
	"git.maik.ch/nullmodem/bbs/internal/message"
)

// threadKludges fills in each outgoing echo's REPLY kludge, so other
// systems thread it under what it answers, and records the MSGID our
// own posts go out under, so their replies find them here. A reply
// only guessed from its subject gets no REPLY.
func threadKludges(messages *message.Store, echoOrigAddr mail.Address, lists ...[]message.PendingEcho) error {
	for _, list := range lists {
		for i := range list {
			m := &list[i]
			if !m.IsFromRemote() {
				if err := messages.SetOutMsgID(m.ID, localMsgID(echoOrigAddr, m.ID)); err != nil {
					return err
				}
			}
			switch {
			case m.ReplyTo.Valid && !m.ReplyGuess:
				parent, err := messages.MessageByID(m.ReplyTo.Int64)
				if err != nil {
					continue // expired meanwhile
				}
				if parent.IsFromRemote() {
					m.ReplyKludge = parent.MsgID
				} else {
					m.ReplyKludge = localMsgID(echoOrigAddr, parent.ID)
				}
			case m.ReplyMsgID != "":
				m.ReplyKludge = m.ReplyMsgID
			}
		}
	}
	return nil
}

// localMsgID is the MSGID a local post goes out under (see buildPacket).
func localMsgID(echoOrigAddr mail.Address, id int64) string {
	return fmt.Sprintf("%s %08x", echoOrigAddr.String(), id)
}

func replyLine(m message.PendingEcho) string {
	if m.ReplyKludge == "" {
		return ""
	}
	return "\x01REPLY: " + m.ReplyKludge + "\r"
}
