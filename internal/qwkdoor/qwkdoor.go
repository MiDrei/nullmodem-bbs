// Package qwkdoor is the BBS side of QWK: it gathers a user's mail
// into a packet, commits the read pointers that implies, and routes
// an uploaded reply packet back into the right areas.
//
// The wire format itself lives in bbskit/qwk, shared with the offline
// reader. What is left here is everything that needs this system's
// message, netmail and user stores -- which is precisely what could
// not be shared.
package qwkdoor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/qwk"
)

// BBSID derives a short, filename-safe system identifier for
// CONTROL.DAT, the .QWK packet's own filename, and the .REP filename
// an offline reader is expected to produce -- uppercased, non-
// alphanumeric characters stripped, truncated to 8 characters (the
// format's own conventional limit, inherited from DOS 8.3 filenames).
func BBSID(bbsName string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(bbsName) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	id := b.String()
	if id == "" {
		id = "BBS"
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

// BuildResult reports what BuildPacketForUser produced: PacketPath is
// empty and MessageCount is 0 when there was nothing new to include.
// UnreadNetmailIDs/MarkRead are the read-state bookkeeping a caller
// should apply via CommitRead once delivery has actually succeeded --
// building the packet never itself marks anything read.
type BuildResult struct {
	PacketPath       string
	MessageCount     int
	UnreadNetmailIDs []int64
	MarkRead         map[int64][]int64 // areaID -> message IDs
}

// BuildPacketForUser gathers u's unread netmail plus unread messages
// in u's areas (message.Store.InMyAreas: every readable area they
// didn't take out) and writes a .QWK packet into dir, shared by both
// the Telnet/SSH qwk builtin (internal/bbs) and the web portal's HTTP
// QWK endpoints (internal/web) so the two surfaces can't drift.
//
// Each message's QWK number is its database ID (netmail's own ID in
// conference 0), so the same message keeps the same number in every
// packet it could ever appear in -- an offline reader merges packets
// and keeps its read markers by (conference, number), and numbers that
// restarted at 1 in each packet made new mail look already read. The
// position within the packet goes in LogicalNumber instead.
func BuildPacketForUser(messages *message.Store, nm *netmail.Store, u *user.User, bbsName, sysopName, dir string) (BuildResult, error) {
	bbsID := BBSID(bbsName)
	callerName := u.RealName
	if callerName == "" {
		callerName = u.Username
	}

	var packed []qwk.PackedMessage
	conferences := []qwk.ConferenceInfo{{Number: 0, Name: "Personal"}}

	inbox, err := nm.Inbox(u.ID)
	if err != nil {
		return BuildResult{}, fmt.Errorf("qwk: loading netmail: %w", err)
	}
	var unreadNetmailIDs []int64
	for _, m := range inbox {
		if m.ReadAt.Valid {
			continue
		}
		// A sender on another FTN system is named with their address,
		// "Name@zone:net/node" -- the form RouteReplies accepts -- so
		// replying from an offline reader reaches them without anyone
		// having to look the address up.
		from := m.FromName
		if !m.FromUserID.Valid && m.FromAddress != "" {
			from = m.FromName + "@" + m.FromAddress
		}
		packed = append(packed, qwk.PackedMessage{
			Header: qwk.MessageHeader{
				Status:        ' ',
				Number:        int(m.ID),
				LogicalNumber: len(packed) + 1,
				Written:       m.PostedAt,
				To:            m.ToName,
				From:          from,
				Subject:       m.Subject,
				Conference:    0,
			},
			Text: qwk.AddKludges(m.ToName, from, m.Subject, message.StripSeenByAndPathForDisplay(m.Body)),
		})
		unreadNetmailIDs = append(unreadNetmailIDs, m.ID)
	}

	areaStats, err := messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		return BuildResult{}, fmt.Errorf("qwk: listing areas: %w", err)
	}
	mine, err := messages.InMyAreas(u.ID)
	if err != nil {
		return BuildResult{}, fmt.Errorf("qwk: loading the caller's areas: %w", err)
	}

	markRead := map[int64][]int64{}
	for _, st := range areaStats {
		if st.New == 0 {
			continue
		}
		if !mine(st.Area.ID) {
			continue
		}
		conferences = append(conferences, qwk.ConferenceInfo{Number: int(st.Area.ID), Name: st.Area.Name})

		areaMessages, err := messages.ListMessages(st.Area.ID)
		if err != nil {
			return BuildResult{}, fmt.Errorf("qwk: listing messages for area %d: %w", st.Area.ID, err)
		}
		readIDs, err := messages.ReadMessageIDs(u.ID, st.Area.ID)
		if err != nil {
			return BuildResult{}, fmt.Errorf("qwk: loading read state for area %d: %w", st.Area.ID, err)
		}
		for _, m := range areaMessages {
			if readIDs[m.ID] {
				continue
			}
			// The SEEN-BY/PATH block only for those who asked
			// (User.QWKRouting): NullModem Reader hides it and can
			// quote it into a reply, the routing an echo ping or a
			// dupe hunt is about -- most other readers would just
			// show it as text.
			body := message.StripSeenByAndPathForDisplay(m.Body)
			if u.QWKRouting {
				body = m.Body
			}
			packed = append(packed, qwk.PackedMessage{
				Header: qwk.MessageHeader{
					Status:        ' ',
					Number:        int(m.ID),
					RefNumber:     int(m.ReplyTo.Int64), // 0 if none: lets the reader thread
					LogicalNumber: len(packed) + 1,
					Written:       m.PostedAt,
					To:            m.ToName,
					From:          m.FromName,
					Subject:       m.Subject,
					Conference:    int(st.Area.ID),
				},
				Text: qwk.AddKludges(m.ToName, m.FromName, m.Subject, body),
			})
			markRead[st.Area.ID] = append(markRead[st.Area.ID], m.ID)
		}
	}

	if len(packed) == 0 {
		return BuildResult{}, nil
	}

	control := qwk.ControlInfo{
		BBSName:    bbsName,
		SysopName:  sysopName,
		BBSID:      bbsID,
		PacketTime: time.Now(),
		CallerName: callerName,
		// A netmail's own To field is typically the recipient's plain
		// username, not their real name -- PERSONAL.NDX needs to
		// recognize both.
		PersonalNames: []string{u.Username},
		Conferences:   conferences,
		Username:      u.Username,
		// Conference 0 is netmail: flagged in TOREADER.EXT, a QWKE
		// reader asks for a recipient there instead of addressing
		// "All" -- and knows where a new netmail goes even when this
		// packet carries none.
		Areas: []qwk.AreaEntry{{Number: 0, Flags: "N"}},
	}

	packetPath := filepath.Join(dir, bbsID+".QWK")
	if err := qwk.BuildQWKPacket(packetPath, control, packed); err != nil {
		return BuildResult{}, fmt.Errorf("qwk: building packet: %w", err)
	}
	return BuildResult{
		PacketPath:       packetPath,
		MessageCount:     len(packed),
		UnreadNetmailIDs: unreadNetmailIDs,
		MarkRead:         markRead,
	}, nil
}

// CommitRead marks every message a successfully-delivered QWK packet
// included as read, so the next pull only contains genuinely new mail
// -- the same bookkeeping opening a message any other way already
// performs.
func CommitRead(messages *message.Store, nm *netmail.Store, userID int64, unreadNetmailIDs []int64, markRead map[int64][]int64) error {
	for _, id := range unreadNetmailIDs {
		if err := nm.MarkRead(id); err != nil {
			return err
		}
	}
	for _, ids := range markRead {
		for _, id := range ids {
			if err := messages.MarkMessageRead(userID, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// RouteResult reports what RouteReplies did with a reply packet.
type RouteResult struct {
	Posted, Sent int
	// Rejected are the replies that were not delivered, with why --
	// so an offline reader can keep them and tell its user instead of
	// losing them silently.
	Rejected []Rejected
}

// Rejected is one reply RouteReplies did not deliver.
type Rejected struct {
	// Index is the reply's position in the packet, from 0.
	Index   int    `json:"index"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
}

// RouteReplies applies every reply parsed from a .REP packet: a reply
// whose (repurposed) conference number is 0 goes to netmail (see
// netmailRecipient for who it reaches); any other conference number is
// posted into the message area whose own ID matches it (rejected if
// the area doesn't exist, is pending, or the caller lacks write
// access).
//
// QWKE kludge lines at the top of a reply carry the untruncated To and
// Subject (the header fields hold 25 bytes); they are used instead of
// the header and stripped from the text, so they never end up in the
// posted message.
func RouteReplies(messages *message.Store, nm *netmail.Store, users *user.Store, ftnAddress string, u *user.User, replies []qwk.PackedMessage) (RouteResult, error) {
	var res RouteResult
	for i, reply := range replies {
		conference := reply.Header.Number // REP repurposes this field -- see qwk.MessageHeader's doc comment
		k, text := qwk.ParseQWKEKludges(reply.Text)
		to := strings.TrimSpace(firstNonEmpty(k.To, reply.Header.To))
		subject := strings.TrimSpace(firstNonEmpty(k.Subject, reply.Header.Subject))
		reject := func(reason string) {
			res.Rejected = append(res.Rejected, Rejected{Index: i, To: to, Subject: subject, Reason: reason})
		}

		if conference == 0 {
			toUserID, toName, toAddress, ok := netmailRecipient(users, to)
			if !ok {
				reject(fmt.Sprintf("unknown recipient %q -- use a username on this BBS or Name@zone:net/node", to))
				continue
			}
			if _, err := nm.Send(u.ID, ftnAddress, toUserID, toName, toAddress, subject, text, false); err != nil {
				return res, fmt.Errorf("qwk: sending netmail reply: %w", err)
			}
			res.Sent++
			continue
		}

		area, err := messages.AreaByID(int64(conference))
		switch {
		case err != nil:
			reject(fmt.Sprintf("conference %d does not exist here", conference))
			continue
		case area.Pending:
			reject(fmt.Sprintf("%s is still awaiting the sysop's approval", area.Name))
			continue
		case !area.CanWrite(u.SecurityLevel):
			reject(fmt.Sprintf("you may not post in %s", area.Name))
			continue
		}
		m, err := messages.PostMessage(area.ID, u.ID, to, subject, text)
		if err != nil {
			return res, fmt.Errorf("qwk: posting reply to area %d: %w", area.ID, err)
		}
		// The reader's "in reply to" is our message number (see the
		// export above), as long as it's in the same area.
		if ref := reply.Header.RefNumber; ref > 0 {
			if parent, err := messages.MessageByID(int64(ref)); err == nil && parent.AreaID == area.ID {
				if err := messages.SetReplyTo(m.ID, parent.ID); err != nil {
					return res, err
				}
			}
		}
		res.Posted++
	}
	return res, nil
}

// netmailRecipient resolves a netmail reply's To, the way the BBS
// portal's own netmail compose does plus the one form QWK readers use
// for a person elsewhere: a username on this BBS; "Name@zone:net/node"
// for someone on another FTN system; or a bare FTN address, which
// reaches that system under the address itself as the name.
func netmailRecipient(users *user.Store, to string) (toUserID int64, toName, toAddress string, ok bool) {
	if to == "" {
		return 0, "", "", false
	}
	if recipient, err := users.ByUsername(to); err == nil {
		return recipient.ID, recipient.Username, "", true
	}
	if at := strings.LastIndex(to, "@"); at > 0 {
		name, addr := strings.TrimSpace(to[:at]), strings.TrimSpace(to[at+1:])
		if name != "" && netmail.IsFTNAddress(addr) {
			return 0, name, addr, true
		}
	}
	if netmail.IsFTNAddress(to) {
		return 0, to, to, true
	}
	return 0, "", "", false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
