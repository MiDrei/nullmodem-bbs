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
// in every area u has selected for QWK (see message.Store's
// QWKSelectedAreaIDs -- an empty selection means every readable area
// with new mail) and writes a .QWK packet into dir, shared by both
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
		packed = append(packed, qwk.PackedMessage{
			Header: qwk.MessageHeader{
				Status:        ' ',
				Number:        int(m.ID),
				LogicalNumber: len(packed) + 1,
				Written:       m.PostedAt,
				To:            m.ToName,
				From:          m.FromName,
				Subject:       m.Subject,
				Conference:    0,
			},
			Text: qwk.AddKludges(m.ToName, m.FromName, m.Subject, message.StripSeenByAndPathForDisplay(m.Body)),
		})
		unreadNetmailIDs = append(unreadNetmailIDs, m.ID)
	}

	areaStats, err := messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		return BuildResult{}, fmt.Errorf("qwk: listing areas: %w", err)
	}
	selected, err := messages.QWKSelectedAreaIDs(u.ID)
	if err != nil {
		return BuildResult{}, fmt.Errorf("qwk: loading area selection: %w", err)
	}

	markRead := map[int64][]int64{}
	for _, st := range areaStats {
		if st.New == 0 {
			continue
		}
		if len(selected) > 0 && !selected[st.Area.ID] {
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
			packed = append(packed, qwk.PackedMessage{
				Header: qwk.MessageHeader{
					Status:        ' ',
					Number:        int(m.ID),
					LogicalNumber: len(packed) + 1,
					Written:       m.PostedAt,
					To:            m.ToName,
					From:          m.FromName,
					Subject:       m.Subject,
					Conference:    int(st.Area.ID),
				},
				Text: qwk.AddKludges(m.ToName, m.FromName, m.Subject, message.StripSeenByAndPathForDisplay(m.Body)),
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

// RouteReplies applies every reply parsed from a .REP packet: a reply
// whose (repurposed) conference number is 0 goes to netmail, resolved
// exactly the way the BBS portal's own netmail compose already
// resolves a recipient -- a local username first, else a raw FTN
// address; any other conference number is posted into the message
// area whose own ID matches it (rejected if the area doesn't exist,
// is pending, or the caller lacks write access). It returns how many
// replies landed in each category.
func RouteReplies(messages *message.Store, nm *netmail.Store, users *user.Store, ftnAddress string, u *user.User, replies []qwk.PackedMessage) (posted, sent, skipped int, err error) {
	for _, reply := range replies {
		conference := reply.Header.Number // REP repurposes this field -- see qwk.MessageHeader's doc comment

		if conference == 0 {
			toName := strings.TrimSpace(reply.Header.To)
			var toUserID int64
			var toAddress string
			if recipient, err := users.ByUsername(toName); err == nil {
				toUserID = recipient.ID
				toName = recipient.Username
			} else if netmail.IsFTNAddress(toName) {
				toAddress = toName
			} else {
				skipped++
				continue
			}
			if _, err := nm.Send(u.ID, ftnAddress, toUserID, toName, toAddress, reply.Header.Subject, reply.Text, false); err != nil {
				return posted, sent, skipped, fmt.Errorf("qwk: sending netmail reply: %w", err)
			}
			sent++
			continue
		}

		area, err := messages.AreaByID(int64(conference))
		if err != nil || area.Pending || !area.CanWrite(u.SecurityLevel) {
			skipped++
			continue
		}
		if _, err := messages.PostMessage(area.ID, u.ID, reply.Header.To, reply.Header.Subject, reply.Text); err != nil {
			return posted, sent, skipped, fmt.Errorf("qwk: posting reply to area %d: %w", area.ID, err)
		}
		posted++
	}
	return posted, sent, skipped, nil
}
