package bbs

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// isFTNAddress is a cheap heuristic -- not full FTN validation -- for
// telling "the caller typed a local username" apart from "the caller
// typed a FidoNet routing address" (zone:net/node[.point], e.g.
// "1:234/56" or "1:234/56.1") when resolving a netmail recipient.
func isFTNAddress(s string) bool {
	zoneRest := strings.SplitN(s, ":", 2)
	if len(zoneRest) != 2 {
		return false
	}
	if _, err := strconv.Atoi(zoneRest[0]); err != nil {
		return false
	}
	netNode := strings.SplitN(zoneRest[1], "/", 2)
	if len(netNode) != 2 {
		return false
	}
	if _, err := strconv.Atoi(netNode[0]); err != nil {
		return false
	}
	nodePoint := strings.SplitN(netNode[1], ".", 2)
	if _, err := strconv.Atoi(nodePoint[0]); err != nil {
		return false
	}
	if len(nodePoint) == 2 {
		if _, err := strconv.Atoi(nodePoint[1]); err != nil {
			return false
		}
	}
	return true
}

// showNetmail is the "builtin:netmail" command: a lightbar over the
// caller's own netmail inbox, mirroring messages.go's browseArea --
// arrow keys move the highlight, Enter opens the reader (marking that
// message read), C composes a new outgoing message, Q/Escape returns
// to the main menu. Unlike echo areas there's only ever one inbox per
// user, so there's no separate area-selection lightbar first.
func (s *Server) showNetmail(term *Terminal, u *user.User) error {
	selected := 0
outer:
	for {
		msgs, err := s.Netmail.Inbox(u.ID)
		if err != nil {
			return err
		}

		if len(msgs) == 0 {
			if err := s.drawEmptyNetmailList(term, u); err != nil {
				return err
			}
			key, err := term.ReadKey()
			if err != nil {
				return err
			}
			switch {
			case key.Type == KeyChar && (key.Rune == 'c' || key.Rune == 'C'):
				if err := s.composeNetmail(term, u); err != nil {
					return err
				}
			case key.Type == KeyEscape:
				return nil
			case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
				return nil
			}
			continue
		}
		if selected >= len(msgs) {
			selected = len(msgs) - 1
		}

		for {
			if err := s.drawNetmailList(term, u, msgs, selected); err != nil {
				return err
			}
			key, err := term.ReadKey()
			if err != nil {
				return err
			}
			switch {
			case key.Type == KeyUp:
				selected = (selected - 1 + len(msgs)) % len(msgs)
			case key.Type == KeyDown:
				selected = (selected + 1) % len(msgs)
			case key.Type == KeyEnter:
				if err := s.readNetmail(term, u, msgs, selected); err != nil {
					return err
				}
				continue outer
			case key.Type == KeyChar && (key.Rune == 'c' || key.Rune == 'C'):
				if err := s.composeNetmail(term, u); err != nil {
					return err
				}
				continue outer
			case key.Type == KeyEscape:
				return nil
			case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
				return nil
			}
		}
	}
}

// netmailListScreen is the hand-designed banner shown above the
// netmail inbox -- see messages.go's msgListScreen doc comment for
// why this clear-screen-then-banner convention matters.
const netmailListScreen = "netmail.ans"

// printNetmailListHeader shows netmail.ans (with USERNAME filled in),
// falling back to a plain colored title line on a cleared screen.
func (s *Server) printNetmailListHeader(term *Terminal, u *user.User) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, netmailListScreen))
	if err != nil {
		return term.Println(ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + "Netmail" + ansi.Reset)
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"USERNAME": u.Username,
	}
	rendered := ansi.Render(raw, vars)
	return term.Println(ansi.Layout(rendered, term.Width()))
}

// drawEmptyNetmailList shows just the header banner and a hint bar
// for an empty inbox -- see messages.go's drawEmptyMessageList.
func (s *Server) drawEmptyNetmailList(term *Terminal, u *user.User) error {
	if err := s.printNetmailListHeader(term, u); err != nil {
		return err
	}
	return term.Print(ansi.Reset + "\n(no netmail yet)\r\n\r\n" + ansi.FG(ansi.White, true) + "[C] Compose   [Q] Back" + ansi.Reset)
}

// Fixed filenames for the hand-designed pieces of the netmail inbox
// lightbar, mirroring messages.go's msglist-columns.ans/-row.ans/
// -row-selected.ans -- see that doc comment for why these are
// separate, customizable screen files with a plain fallback.
const (
	netmailListColumnsScreen     = "netmail-columns.ans"
	netmailListRowScreen         = "netmail-row.ans"
	netmailListRowSelectedScreen = "netmail-row-selected.ans"
)

const (
	fallbackNetmailListRow         = "\x1b[1;33m{NEWFLAG:-3} \x1b[0m{SUBJECT:-39} {FROM:-18} {DATE:16}"
	fallbackNetmailListRowSelected = "\x1b[47m\x1b[30m{NEWFLAG:-3} {SUBJECT:-39} {FROM:-18} {DATE:16}\x1b[0m"
)

var fallbackNetmailListColumns = "    Subject                                 From                           Date\r\n" + strings.Repeat("-", 79)

// drawNetmailList redraws the header banner plus the Subject/From/
// Date table, with the row at selected highlighted and any unread
// message flagged via NEWFLAG -- the netmail inbox's equivalent of
// messages.go's drawMessageList.
func (s *Server) drawNetmailList(term *Terminal, u *user.User, msgs []netmail.Message, selected int) error {
	if err := s.printNetmailListHeader(term, u); err != nil {
		return err
	}

	rowTemplate := s.loadOptionalScreen(netmailListRowScreen, fallbackNetmailListRow)
	rowSelectedTemplate := s.loadOptionalScreen(netmailListRowSelectedScreen, fallbackNetmailListRowSelected)

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(s.loadOptionalScreen(netmailListColumnsScreen, fallbackNetmailListColumns))
	b.WriteString(ansi.CRLF)

	for i, m := range msgs {
		tmpl := rowTemplate
		if i == selected {
			tmpl = rowSelectedTemplate
		}
		newFlag := ""
		if !m.IsRead() {
			newFlag = "NEW"
		}
		vars := ansi.Vars{
			"SUBJECT": m.Subject,
			"FROM":    m.FromName,
			"DATE":    m.PostedAt.Format("2006-01-02 15:04"),
			"NEWFLAG": newFlag,
		}
		b.WriteString(ansi.Render(tmpl, vars))
		b.WriteString(ansi.CRLF)
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + "[Up/Down] Move   [Enter] Read   [C] Compose   [Q] Back" + ansi.Reset)
	return term.Print(b.String())
}

// Fixed filenames for the hand-designed pieces of the netmail reader,
// mirroring messages.go's msgread.ans/msgread-meta.ans.
const (
	netmailReadScreen     = "netread.ans"
	netmailReadMetaScreen = "netread-meta.ans"
)

var fallbackNetmailReadMeta = "\x1b[1;35mFrom:    \x1b[1;37m{FROM:-40}\x1b[1;35m Date: \x1b[1;37m{DATE}\r\n" +
	"\x1b[1;35mTo:      \x1b[1;37m{TO:-40}\r\n" +
	"\x1b[1;35mSubject: \x1b[1;37m{SUBJECT}\r\n" +
	"\x1b[35m" + strings.Repeat("-", 79) + ansi.Reset

// readNetmail is a reader over msgs, starting at idx, that lets the
// caller page through their inbox with the arrow keys / N,P without
// returning to the list each time -- mirroring messages.go's
// readMessage, including clamping at the first/last message instead
// of wrapping around.
func (s *Server) readNetmail(term *Terminal, u *user.User, msgs []netmail.Message, idx int) error {
	for {
		if err := s.Netmail.MarkRead(msgs[idx].ID); err != nil {
			return err
		}
		if err := s.drawNetmailReader(term, msgs, idx); err != nil {
			return err
		}
		key, err := term.ReadKey()
		if err != nil {
			return err
		}
		switch {
		case key.Type == KeyUp || key.Type == KeyLeft, key.Type == KeyChar && (key.Rune == 'p' || key.Rune == 'P'):
			if idx > 0 {
				idx--
			}
		case key.Type == KeyDown || key.Type == KeyRight || key.Type == KeyEnter, key.Type == KeyChar && (key.Rune == 'n' || key.Rune == 'N'):
			if idx < len(msgs)-1 {
				idx++
			}
		case key.Type == KeyChar && (key.Rune == 'r' || key.Rune == 'R'):
			if err := s.replyToNetmail(term, u, &msgs[idx]); err != nil {
				return err
			}
		case key.Type == KeyEscape:
			return nil
		case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
			return nil
		}
	}
}

// replyToNetmail sends a reply to original's sender: Subject defaults
// to "Re: <original subject>" (see replySubject) and the recipient to
// the original sender -- a local user if original was composed here,
// or back out to their FTN address (via internal/tosser) if it
// arrived from a remote system -- then hands off to the shared
// runLineEditor for the body.
func (s *Server) replyToNetmail(term *Terminal, u *user.User, original *netmail.Message) error {
	subject := replySubject(original.Subject)
	if err := term.Print(ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + "Reply to Netmail" + ansi.Reset); err != nil {
		return err
	}
	if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + "To: " + ansi.Reset + original.FromName); err != nil {
		return err
	}
	if err := term.Println(ansi.FG(ansi.Cyan, true) + "Subject: " + ansi.Reset + subject); err != nil {
		return err
	}

	lines, saved, err := s.runLineEditor(term)
	if err != nil {
		return err
	}
	if !saved {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Reply aborted.")
	}

	var toUserID int64
	var toAddress string
	if original.FromUserID.Valid {
		toUserID = original.FromUserID.Int64
	} else {
		toAddress = original.FromAddress
	}
	if _, err := s.Netmail.Send(u.ID, s.FTNAddress, toUserID, original.FromName, toAddress, subject, strings.Join(lines, "\n"), false); err != nil {
		return err
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Reply sent.")
}

// printNetmailReaderHeader shows netread.ans (with MSGNUM/MSGCOUNT
// filled in), falling back to a plain title line -- mirroring
// messages.go's printMessageReaderHeader.
func (s *Server) printNetmailReaderHeader(term *Terminal, idx, total int) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, netmailReadScreen))
	if err != nil {
		return term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + "Netmail" + ansi.Reset)
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"MSGNUM":   strconv.Itoa(idx + 1),
		"MSGCOUNT": strconv.Itoa(total),
	}
	rendered := ansi.Render(raw, vars)
	return term.Println(ansi.Layout(rendered, term.Width()))
}

// drawNetmailReader redraws the full reader screen for msgs[idx]: the
// header banner, the From/To/Subject/Date metadata block, the word-
// wrapped body, and a footer hinting at the navigation keys --
// mirroring messages.go's drawMessageReader.
func (s *Server) drawNetmailReader(term *Terminal, msgs []netmail.Message, idx int) error {
	if err := s.printNetmailReaderHeader(term, idx, len(msgs)); err != nil {
		return err
	}
	m := &msgs[idx]

	to := m.ToName
	if m.ToAddress != "" {
		to = fmt.Sprintf("%s (%s)", m.ToName, m.ToAddress)
	}
	metaTemplate := s.loadOptionalScreen(netmailReadMetaScreen, fallbackNetmailReadMeta)
	vars := ansi.Vars{
		"FROM":    m.FromName,
		"TO":      to,
		"SUBJECT": m.Subject,
		"DATE":    m.PostedAt.Format("2006-01-02 15:04"),
	}

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(ansi.Layout(ansi.Render(metaTemplate, vars), term.Width()))
	b.WriteString(ansi.CRLF)
	for _, line := range ansi.WrapText(m.Body, term.Width()) {
		b.WriteString(ansi.Reset + line + ansi.CRLF)
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + "[Enter/Dn/Right] Next  [Up/Left] Prev  [R] Reply  [Q] Back to list" + ansi.Reset)
	return term.Print(b.String())
}

// composeNetmail prompts for a recipient (an existing local username,
// or an FTN address for a system this BBS can't reach without a
// BinkP mailer yet -- see internal/netmail's doc comment), and for an
// FTN address also the recipient's name (a node has many possible
// recipients; the address alone doesn't say who a remote sysop should
// hand the message to), then a Subject, then hands off to the shared
// runLineEditor for the body.
func (s *Server) composeNetmail(term *Terminal, u *user.User) error {
	if err := term.Print(ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + "Compose Netmail" + ansi.Reset); err != nil {
		return err
	}
	if err := term.Print(ansi.Reset + "\nTo (username or FTN address zone:net/node.point): " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	to, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	var toUserID int64
	var toName, toAddress string
	var crash bool
	if recipient, err := s.Users.ByUsername(to); err == nil {
		toUserID = recipient.ID
		toName = recipient.Username
	} else if !errors.Is(err, user.ErrNotFound) {
		return err
	} else if isFTNAddress(to) {
		toAddress = to
		if err := term.Print(ansi.Reset + "Recipient name: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		name, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		toName = strings.TrimSpace(name)
		if toName == "" {
			toName = to
		}
		if err := term.Print(ansi.Reset + "Crash priority (immediate delivery)? [y/N]: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		crashAnswer, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		crashAnswer = strings.ToLower(strings.TrimSpace(crashAnswer))
		crash = crashAnswer == "y" || crashAnswer == "yes"
	} else {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) +
			fmt.Sprintf("No such local user, and %q doesn't look like an FTN address (zone:net/node.point).", to))
	}

	if err := term.Print(ansi.Reset + "Subject: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	subject, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	lines, saved, err := s.runLineEditor(term)
	if err != nil {
		return err
	}
	if !saved {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Message aborted.")
	}

	if _, err := s.Netmail.Send(u.ID, s.FTNAddress, toUserID, toName, toAddress, subject, strings.Join(lines, "\n"), crash); err != nil {
		return err
	}
	if toUserID > 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Netmail sent.")
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) +
		fmt.Sprintf("Netmail queued for %s at %s -- no BinkP mailer is configured yet, so it will be delivered once one is set up.", toName, toAddress))
}
