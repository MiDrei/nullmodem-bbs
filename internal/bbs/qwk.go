package bbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/qwk"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
	"git.maik.ch/swissmaik/nullmodem/internal/zmodem"
)

// qwkBBSID derives a short, filename-safe system identifier for QWK's
// CONTROL.DAT, the .QWK packet's own filename, and the .REP filename
// an offline reader is expected to produce -- uppercased, non-
// alphanumeric characters stripped, truncated to 8 characters (the
// format's own conventional limit, inherited from DOS 8.3 filenames).
func qwkBBSID(bbsName string) string {
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

// downloadQWK is the "builtin:qwk" command: it gathers every unread
// message in every area the caller can read, plus their unread
// netmail, into a single .QWK offline-mail packet (see internal/qwk's
// own package doc comment for the wire format) and sends it via
// Zmodem, mirroring downloadFile's own Terminal.Raw/PushBack handling
// exactly. A message area maps to a QWK conference by its own,
// already-stable Area.ID; netmail is conference 0. Everything
// included is marked read as part of a successful download, the same
// as opening it any other way already does, so the next pull only
// contains genuinely new mail.
func (s *Server) downloadQWK(term *Terminal, u *user.User) error {
	tmpDir, err := os.MkdirTemp("", "nullmodem-qwk-*")
	if err != nil {
		return fmt.Errorf("qwk download: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	bbsID := qwkBBSID(s.BBSName)
	callerName := u.RealName
	if callerName == "" {
		callerName = u.Username
	}

	var messages []qwk.PackedMessage
	conferences := []qwk.ConferenceInfo{{Number: 0, Name: "Personal"}}

	inbox, err := s.Netmail.Inbox(u.ID)
	if err != nil {
		return fmt.Errorf("qwk download: loading netmail: %w", err)
	}
	var unreadNetmailIDs []int64
	for _, m := range inbox {
		if m.ReadAt.Valid {
			continue
		}
		messages = append(messages, qwk.PackedMessage{
			Header: qwk.MessageHeader{
				Status:     ' ',
				Number:     len(messages) + 1,
				Written:    m.PostedAt,
				To:         m.ToName,
				From:       m.FromName,
				Subject:    m.Subject,
				Conference: 0,
			},
			Text: message.StripSeenByAndPathForDisplay(m.Body),
		})
		unreadNetmailIDs = append(unreadNetmailIDs, m.ID)
	}

	areaStats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		return fmt.Errorf("qwk download: listing areas: %w", err)
	}
	markRead := map[int64][]int64{} // areaID -> message IDs to mark read after a successful send
	for _, st := range areaStats {
		if st.New == 0 {
			continue
		}
		conferences = append(conferences, qwk.ConferenceInfo{Number: int(st.Area.ID), Name: st.Area.Name})

		areaMessages, err := s.Messages.ListMessages(st.Area.ID)
		if err != nil {
			return fmt.Errorf("qwk download: listing messages for area %d: %w", st.Area.ID, err)
		}
		readIDs, err := s.Messages.ReadMessageIDs(u.ID, st.Area.ID)
		if err != nil {
			return fmt.Errorf("qwk download: loading read state for area %d: %w", st.Area.ID, err)
		}
		for _, m := range areaMessages {
			if readIDs[m.ID] {
				continue
			}
			messages = append(messages, qwk.PackedMessage{
				Header: qwk.MessageHeader{
					Status:     ' ',
					Number:     len(messages) + 1,
					Written:    m.PostedAt,
					To:         m.ToName,
					From:       m.FromName,
					Subject:    m.Subject,
					Conference: int(st.Area.ID),
				},
				Text: message.StripSeenByAndPathForDisplay(m.Body),
			})
			markRead[st.Area.ID] = append(markRead[st.Area.ID], m.ID)
		}
	}

	if len(messages) == 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) + "No new mail to download.")
	}

	control := qwk.ControlInfo{
		BBSName:    s.BBSName,
		SysopName:  s.SysopName,
		BBSID:      bbsID,
		PacketTime: time.Now(),
		CallerName: callerName,
		// A netmail's own To field is typically the recipient's plain
		// username (see e.g. handleSendBBSNetmail's own resolution),
		// not their real name -- PERSONAL.NDX needs to recognize both.
		PersonalNames: []string{u.Username},
		Conferences:   conferences,
	}

	packetPath := filepath.Join(tmpDir, bbsID+".QWK")
	if err := qwk.BuildQWKPacket(packetPath, control, messages); err != nil {
		return fmt.Errorf("qwk download: building packet: %w", err)
	}
	info, err := os.Stat(packetPath)
	if err != nil {
		return fmt.Errorf("qwk download: stat packet: %w", err)
	}

	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		fmt.Sprintf("Starting Zmodem download of your QWK packet (%s, %s) -- your terminal should start receiving automatically.", filepath.Base(packetPath), humanize.Bytes(uint64(info.Size()))) +
		ansi.Reset + "\r\n"); err != nil {
		return err
	}

	leftover, sendErr := zmodem.Send(term.Raw(), packetPath)
	if len(leftover) > 0 {
		term.PushBack(leftover)
	}
	if sendErr != nil {
		s.logWarn("zmodem QWK download by %s: %v", u.Username, sendErr)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Download failed or was cancelled.")
	}

	for _, id := range unreadNetmailIDs {
		if err := s.Netmail.MarkRead(id); err != nil {
			return err
		}
	}
	for _, ids := range markRead {
		for _, id := range ids {
			if err := s.Messages.MarkMessageRead(u.ID, id); err != nil {
				return err
			}
		}
	}

	s.logInfo("%s downloaded a QWK packet: %d message(s)", u.Username, len(messages))
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Download complete: %d new message(s).", len(messages)))
}

// uploadQWKReply is the "builtin:qwkrep" command: it receives a .REP
// reply packet via Zmodem (mirroring uploadFile's own scratch-dir/
// Terminal.Raw/PushBack handling exactly) and routes each reply
// either into netmail (conference 0, recipient resolved exactly the
// way the BBS portal's own netmail compose already does -- a local
// username first, else a raw FTN address) or the message area whose
// own ID matches the reply's conference number -- see
// qwk.MessageHeader's own doc comment for why the REP format
// repurposes its Number field to mean "conference" here rather than a
// real message number.
func (s *Server) uploadQWKReply(term *Terminal, u *user.User) error {
	tmpDir, err := os.MkdirTemp("", "nullmodem-qwkrep-*")
	if err != nil {
		return fmt.Errorf("qwk upload: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		"Ready to receive your QWK reply packet via Zmodem -- start the upload in your terminal now." +
		ansi.Reset + "\r\n"); err != nil {
		return err
	}

	names, leftover, recvErr := zmodem.Receive(term.Raw(), tmpDir)
	if len(leftover) > 0 {
		term.PushBack(leftover)
	}

	var repName string
	for _, name := range names {
		if strings.EqualFold(filepath.Ext(name), ".rep") {
			repName = name
			break
		}
	}
	if repName == "" {
		if recvErr != nil {
			s.logWarn("zmodem QWK reply upload by %s: %v", u.Username, recvErr)
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Upload failed or was cancelled.")
		}
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "No .REP file was received.")
	}

	bbsID := qwkBBSID(s.BBSName)
	replies, err := qwk.ParseReplyPacket(filepath.Join(tmpDir, repName), bbsID)
	if err != nil {
		s.logWarn("parsing QWK reply packet from %s: %v", u.Username, err)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Could not read that reply packet.")
	}

	var sent, posted, skipped int
	for _, reply := range replies {
		conference := reply.Header.Number // REP repurposes this field -- see doc comment above

		if conference == 0 {
			toName := strings.TrimSpace(reply.Header.To)
			var toUserID int64
			var toAddress string
			if recipient, err := s.Users.ByUsername(toName); err == nil {
				toUserID = recipient.ID
				toName = recipient.Username
			} else if netmail.IsFTNAddress(toName) {
				toAddress = toName
			} else {
				skipped++
				continue
			}
			if _, err := s.Netmail.Send(u.ID, s.FTNAddress, toUserID, toName, toAddress, reply.Header.Subject, reply.Text, false); err != nil {
				return fmt.Errorf("qwk upload: sending netmail reply: %w", err)
			}
			sent++
			continue
		}

		area, err := s.Messages.AreaByID(int64(conference))
		if err != nil || area.Pending || !area.CanWrite(u.SecurityLevel) {
			skipped++
			continue
		}
		if _, err := s.Messages.PostMessage(area.ID, u.ID, reply.Header.To, reply.Header.Subject, reply.Text); err != nil {
			return fmt.Errorf("qwk upload: posting reply to area %d: %w", area.ID, err)
		}
		posted++
	}

	s.logInfo("%s uploaded a QWK reply packet: %d posted, %d netmail sent, %d skipped", u.Username, posted, sent, skipped)

	msg := ansi.Reset + "\r\n" + ansi.FG(ansi.Green, true) +
		fmt.Sprintf("Replies processed: %d posted, %d netmail sent", posted, sent)
	if skipped > 0 {
		msg += fmt.Sprintf(", %d skipped", skipped)
	}
	return term.Println(msg)
}
