package bbs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"

	"git.maik.ch/nullmodem/bbs/internal/qwkdoor"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
	"git.maik.ch/nullmodem/kit/qwk"
	"git.maik.ch/nullmodem/kit/zmodem"
)

// buildQWKPacketForUser is a thin wrapper around qwkdoor.BuildPacketForUser
// binding it to this Server's own stores/identity, shared (via the
// qwk package) with the web portal's HTTP QWK endpoints.
func (s *Server) buildQWKPacketForUser(u *user.User, dir string) (packetPath string, messageCount int, unreadNetmailIDs []int64, markRead map[int64][]int64, err error) {
	result, err := qwkdoor.BuildPacketForUser(s.Messages, s.Netmail, u, s.BBSName, s.SysopName, dir)
	if err != nil {
		return "", 0, nil, nil, err
	}
	return result.PacketPath, result.MessageCount, result.UnreadNetmailIDs, result.MarkRead, nil
}

// commitQWKRead is a thin wrapper around qwkdoor.CommitRead binding it to
// this Server's own stores.
func (s *Server) commitQWKRead(userID int64, unreadNetmailIDs []int64, markRead map[int64][]int64) error {
	return qwkdoor.CommitRead(s.Messages, s.Netmail, userID, unreadNetmailIDs, markRead)
}

// routeQWKReplies is a thin wrapper around qwkdoor.RouteReplies binding it
// to this Server's own stores/identity.
func (s *Server) routeQWKReplies(u *user.User, replies []qwk.PackedMessage) (qwkdoor.RouteResult, error) {
	return qwkdoor.RouteReplies(s.Messages, s.Netmail, s.Users, s.FTNAddress, u, replies)
}

// downloadQWK is the "builtin:qwk" command: it builds the caller's
// current QWK packet (see buildQWKPacketForUser) and sends it via
// Zmodem, mirroring downloadFile's own Terminal.Raw/PushBack handling
// exactly.
func (s *Server) downloadQWK(term *Terminal, u *user.User) error {
	tmpDir, err := os.MkdirTemp("", "nullmodem-qwk-*")
	if err != nil {
		return fmt.Errorf("qwk download: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	packetPath, count, unreadNetmailIDs, markRead, err := s.buildQWKPacketForUser(u, tmpDir)
	if err != nil {
		return fmt.Errorf("qwk download: %w", err)
	}
	if count == 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) + "No new mail to download.")
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

	if err := s.commitQWKRead(u.ID, unreadNetmailIDs, markRead); err != nil {
		return err
	}

	s.logInfo("%s downloaded a QWK packet: %d message(s)", u.Username, count)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Download complete: %d new message(s).", count))
}

// uploadQWKReply is the "builtin:qwkrep" command: it receives a .REP
// reply packet via Zmodem (mirroring uploadFile's own scratch-dir/
// Terminal.Raw/PushBack handling exactly), parses it, and routes each
// reply via routeQWKReplies.
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

	bbsID := qwkdoor.BBSID(s.BBSName)
	replies, err := qwk.ParseReplyPacket(filepath.Join(tmpDir, repName), bbsID)
	if err != nil {
		s.logWarn("parsing QWK reply packet from %s: %v", u.Username, err)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Could not read that reply packet.")
	}

	res, err := s.routeQWKReplies(u, replies)
	if err != nil {
		return fmt.Errorf("qwk upload: %w", err)
	}

	s.logInfo("%s uploaded a QWK reply packet: %d posted, %d netmail sent, %d skipped", u.Username, res.Posted, res.Sent, len(res.Rejected))

	msg := ansi.Reset + "\r\n" + ansi.FG(ansi.Green, true) +
		fmt.Sprintf("Replies processed: %d posted, %d netmail sent", res.Posted, res.Sent)
	if len(res.Rejected) > 0 {
		msg += fmt.Sprintf(", %d skipped", len(res.Rejected))
	}
	if err := term.Println(msg); err != nil {
		return err
	}
	// Say which ones and why: the upload is the only moment the
	// caller hears about it, and the offline reader has already
	// dropped them from its queue.
	for _, r := range res.Rejected {
		line := fmt.Sprintf("  Not delivered: %q to %s -- %s", r.Subject, r.To, r.Reason)
		if err := term.Println(ansi.FG(ansi.Red, true) + line + ansi.Reset); err != nil {
			return err
		}
	}
	return nil
}

// configureQWKAreas is the "builtin:qwkareas" command: a numbered
// checklist letting the caller pick exactly which message areas their
// QWK packets include (see message.Store.QWKSelectedAreaIDs/
// buildQWKPacketForUser -- leaving every area unchecked, or never
// visiting this menu at all, includes every readable area with new
// mail).
func (s *Server) configureQWKAreas(term *Terminal, u *user.User) error {
	areaStats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		return err
	}
	if len(areaStats) == 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) + "No message areas available.")
	}
	sort.Slice(areaStats, func(i, j int) bool { return areaStats[i].Area.SortOrder < areaStats[j].Area.SortOrder })

	selected, err := s.Messages.QWKSelectedAreaIDs(u.ID)
	if err != nil {
		return err
	}
	// An empty selection means "everything" -- reflect that in the
	// checklist by starting every box checked, matching what a
	// download would actually include right now.
	checked := make(map[int64]bool, len(areaStats))
	for _, st := range areaStats {
		checked[st.Area.ID] = len(selected) == 0 || selected[st.Area.ID]
	}

	for {
		if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + "QWK area selection -- pick which areas your QWK packets include:" + ansi.Reset); err != nil {
			return err
		}
		for i, st := range areaStats {
			box := "[ ]"
			if checked[st.Area.ID] {
				box = ansi.FG(ansi.Green, true) + "[x]" + ansi.Reset
			}
			if err := term.Println(fmt.Sprintf("%3d. %s %s", i+1, box, st.Area.Name)); err != nil {
				return err
			}
		}
		if err := term.Print(ansi.Reset + "\n" +
			"Enter numbers to toggle (space/comma separated), A=all, N=none/default, S=save, Q=quit: " +
			ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		line, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)

		switch strings.ToUpper(line) {
		case "", "Q":
			return term.Println(ansi.Reset + "Cancelled -- no changes saved.")
		case "A":
			for _, st := range areaStats {
				checked[st.Area.ID] = true
			}
			continue
		case "N":
			for _, st := range areaStats {
				checked[st.Area.ID] = false
			}
			continue
		case "S":
			var ids []int64
			allChecked := true
			for _, st := range areaStats {
				if checked[st.Area.ID] {
					ids = append(ids, st.Area.ID)
				} else {
					allChecked = false
				}
			}
			// Storing "every area checked" is indistinguishable from
			// "none configured" (both mean "everything"), so clear
			// the selection outright in that case rather than writing
			// out every ID -- functionally identical, tidier storage.
			if allChecked {
				ids = nil
			}
			if err := s.Messages.SetQWKSelectedAreas(u.ID, ids); err != nil {
				return err
			}
			s.logInfo("%s updated their QWK area selection: %d area(s)", u.Username, len(ids))
			return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Saved.")
		}

		for _, tok := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ' ' }) {
			n, err := strconv.Atoi(tok)
			if err != nil || n < 1 || n > len(areaStats) {
				continue
			}
			id := areaStats[n-1].Area.ID
			checked[id] = !checked[id]
		}
	}
}
