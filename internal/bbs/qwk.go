package bbs

import (
	"fmt"
	"os"
	"path/filepath"
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
	return qwkdoor.RouteReplies(s.Messages, s.Netmail, s.Users, s.FTNAddress, u, replies, s.emailConfig())
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
		return term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) + term.T("qwk.no_mail"))
	}

	info, err := os.Stat(packetPath)
	if err != nil {
		return fmt.Errorf("qwk download: stat packet: %w", err)
	}

	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		term.T("qwk.download_start", "FILE", filepath.Base(packetPath), "SIZE", humanize.Bytes(uint64(info.Size()))) +
		ansi.Reset + "\r\n"); err != nil {
		return err
	}

	leftover, sendErr := zmodem.Send(term.Raw(), packetPath)
	if len(leftover) > 0 {
		term.PushBack(leftover)
	}
	if sendErr != nil {
		s.logWarn("zmodem QWK download by %s: %v", u.Username, sendErr)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("transfer.download_failed"))
	}

	if err := s.commitQWKRead(u.ID, unreadNetmailIDs, markRead); err != nil {
		return err
	}

	s.logInfo("%s downloaded a QWK packet: %d message(s)", u.Username, count)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.N("qwk.download_done", count))
}

// uploadQWKReply is the "builtin:qwkrep" command: it receives a .REP
// reply packet via Zmodem (mirroring uploadFile's own scratch-dir/
// Terminal.Raw/PushBack handling exactly), parses it, and routes each
// reply via routeQWKReplies.
func (s *Server) uploadQWKReply(term *Terminal, u *user.User) error {
	if ok, err := s.mayPost(term, u); !ok {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "nullmodem-qwkrep-*")
	if err != nil {
		return fmt.Errorf("qwk upload: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		term.T("qwk.upload_ready") +
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
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("transfer.upload_failed"))
		}
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("qwk.no_rep"))
	}

	bbsID := qwkdoor.BBSID(s.BBSName)
	replies, err := qwk.ParseReplyPacket(filepath.Join(tmpDir, repName), bbsID)
	if err != nil {
		s.logWarn("parsing QWK reply packet from %s: %v", u.Username, err)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("qwk.bad_rep"))
	}

	res, err := s.routeQWKReplies(u, replies)
	if err != nil {
		return fmt.Errorf("qwk upload: %w", err)
	}

	s.logInfo("%s uploaded a QWK reply packet: %d posted, %d netmail sent, %d skipped", u.Username, res.Posted, res.Sent, len(res.Rejected))

	msg := ansi.Reset + "\r\n" + ansi.FG(ansi.Green, true) +
		term.T("qwk.processed", "POSTED", res.Posted, "SENT", res.Sent)
	if len(res.Rejected) > 0 {
		msg += term.T("qwk.skipped", "COUNT", len(res.Rejected))
	}
	if err := term.Println(msg); err != nil {
		return err
	}
	// Say which ones and why: the upload is the only moment the
	// caller hears about it, and the offline reader has already
	// dropped them from its queue.
	for _, r := range res.Rejected {
		line := "  " + term.T("qwk.not_delivered", "SUBJECT", r.Subject, "TO", r.To, "REASON", r.Reason)
		if err := term.Println(ansi.FG(ansi.Red, true) + line + ansi.Reset); err != nil {
			return err
		}
	}
	return nil
}
