package bbs

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
)

// mergeNetmailByPostedAt combines a and b (each already sorted newest-
// first, matching netmail.Store.Inbox/UnresolvedInbox's own ordering)
// into one newest-first list.
func mergeNetmailByPostedAt(a, b []netmail.Message) []netmail.Message {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	merged := make([]netmail.Message, 0, len(a)+len(b))
	merged = append(merged, a...)
	merged = append(merged, b...)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].PostedAt.After(merged[j].PostedAt)
	})
	return merged
}

// isFTNAddress is internal/netmail.IsFTNAddress under the name this
// file's other code already uses.
var isFTNAddress = netmail.IsFTNAddress

// unresolvedNetmailLimit bounds how many of the most recent
// unresolved-recipient messages (see netmail.Store.UnresolvedInbox)
// get merged into a sysop's own netmail view -- generous, since this
// is meant to surface robot replies (Areafix/Filefix, ...) promptly,
// not accumulate an unbounded backlog on screen.
const unresolvedNetmailLimit = 50

// showNetmail is the "builtin:netmail" command: a lightbar over the
// caller's own netmail inbox, mirroring messages.go's browseArea --
// arrow keys move the highlight, Enter opens the reader (marking that
// message read), C composes a new outgoing message, Q/Escape returns
// to the main menu. Unlike echo areas there's only ever one inbox per
// user, so there's no separate area-selection lightbar first.
//
// A sysop's own view also merges in unresolved-recipient netmail (see
// netmail.Store.UnresolvedInbox) -- a reply from an Areafix/Filefix
// robot, say, addressed back to whatever name this system used as its
// own request's From, which isn't a real BBS username and so would
// otherwise never appear in anyone's inbox at all.
func (s *Server) showNetmail(term *Terminal, u *user.User) error {
	selected := 0
outer:
	for {
		msgs, err := s.Netmail.Inbox(u.ID)
		if err != nil {
			return err
		}
		if u.SecurityLevel >= user.SLSysop {
			unresolved, err := s.Netmail.UnresolvedInbox(unresolvedNetmailLimit)
			if err != nil {
				return err
			}
			msgs = mergeNetmailByPostedAt(msgs, unresolved)
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
				if selected > 0 {
					selected--
				}
			case key.Type == KeyDown:
				if selected < len(msgs)-1 {
					selected++
				}
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
			case key.Type == KeyChar && (key.Rune == 'd' || key.Rune == 'D'):
				if _, err := s.confirmDeleteNetmail(term, &msgs[selected]); err != nil {
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

// confirmDeleteNetmail prompts before permanently deleting m (see
// netmail.Store.Delete) -- an inbox otherwise has no way to clear
// itself out and just accumulates forever. Reports whether it
// actually deleted (false on a "no" answer or an empty response), so
// a caller mid-reader-navigation knows whether to return to the list
// (msgs no longer includes m) or keep going.
func (s *Server) confirmDeleteNetmail(term *Terminal, m *netmail.Message) (bool, error) {
	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Red, true) + term.T("netmail.delete_confirm", "SUBJECT", m.Subject) + ansi.FG(ansi.Yellow, true)); err != nil {
		return false, err
	}
	answer, err := term.ReadLine(false)
	if err != nil {
		return false, err
	}
	answer = strings.ToUpper(strings.TrimSpace(answer))
	if !isYes(term, answer) && answer != "YES" {
		return false, nil
	}
	if err := s.Netmail.Delete(m.ID); err != nil {
		return false, err
	}
	return true, nil
}

// netmailListScreen is the hand-designed banner shown above the
// netmail inbox -- see messages.go's msgListScreen doc comment for
// why this clear-screen-then-banner convention matters.
const netmailListScreen = "netmail.ans"

// renderNetmailListHeader returns netmail.ans (with USERNAME filled
// in), falling back to a plain colored title line -- mirrors
// messages.go's renderMessageListHeader, including returning a string
// (see finishHeaderLine) rather than printing directly so
// drawNetmailList can count its line count toward the list's scroll
// viewport budget.
func (s *Server) renderNetmailListHeader(term *Terminal, u *user.User) string {
	raw, err := s.loadScreen(term.Lang, netmailListScreen)
	if err != nil {
		return ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + term.T("netmail.title") + ansi.Reset + "\n"
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"USERNAME": u.Username,
	}
	rendered := ansi.Render(raw, vars)
	return finishHeaderLine(ansi.Layout(rendered, term.Width()))
}

// drawEmptyNetmailList shows just the header banner and a hint bar
// for an empty inbox -- see messages.go's drawEmptyMessageList.
func (s *Server) drawEmptyNetmailList(term *Terminal, u *user.User) error {
	return term.Print(s.renderNetmailListHeader(term, u) + ansi.Reset + term.T("netmail.empty") + "\r\n\r\n" + ansi.FG(ansi.White, true) + term.T("netmail.empty_keys") + ansi.Reset)
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

var fallbackNetmailListColumns = "    {T:col.subject:-40}{T:col.from:-30}{T:col.date:5}\r\n" + strings.Repeat("-", 79)

// drawNetmailList redraws the header banner plus the Subject/From/
// Date table, with the row at selected highlighted and any unread
// message flagged via NEWFLAG -- the netmail inbox's equivalent of
// messages.go's drawMessageList, scrolling included: the table is
// windowed to whatever vertical space is left after the header/
// columns/footer, keeping selected in view, and padded with blank
// lines when there are fewer messages than fit so the footer always
// lands on the same row -- see drawMessageList's doc comment for why
// (a long inbox used to just dump every row in one shot, pushing the
// header off the top of the screen).
func (s *Server) drawNetmailList(term *Terminal, u *user.User, msgs []netmail.Message, selected int) error {
	header := s.renderNetmailListHeader(term, u)

	rowTemplate := s.loadOptionalScreen(term, netmailListRowScreen, fallbackNetmailListRow)
	rowSelectedTemplate := s.loadOptionalScreen(term, netmailListRowSelectedScreen, fallbackNetmailListRowSelected)
	columns := s.loadOptionalScreen(term, netmailListColumnsScreen, fallbackNetmailListColumns)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString(ansi.Reset)
	b.WriteString(columns)
	b.WriteString(ansi.CRLF)

	used := strings.Count(header, "\n") + strings.Count(columns, "\n") + 1 + 3
	available := term.Height() - used
	if available < 1 {
		available = 1
	}

	// Top-anchored, not centered (unlike drawMessageList): the inbox is
	// already sorted newest-first (see netmail.Store.Inbox/
	// UnresolvedInbox), so opening it (selected == 0) should show the
	// newest mail at the very top of the screen, with the window only
	// scrolling down far enough to keep selected visible once it's
	// moved past the bottom edge -- not re-centering the whole
	// viewport on every keypress the way the message list does, which
	// would scroll the newest mail out of view almost immediately.
	scrollOffset := selected - available + 1
	if scrollOffset > len(msgs)-available {
		scrollOffset = len(msgs) - available
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	end := scrollOffset + available
	if end > len(msgs) {
		end = len(msgs)
	}

	for i := scrollOffset; i < end; i++ {
		m := msgs[i]
		tmpl := rowTemplate
		if i == selected {
			tmpl = rowSelectedTemplate
		}
		newFlag := ""
		if !m.IsRead() {
			newFlag = term.T("list.new_flag")
		}
		vars := ansi.Vars{
			"SUBJECT": m.Subject,
			"FROM":    m.FromName,
			"DATE":    term.Time(m.PostedAt).Format("2006-01-02 15:04"), // netmail-row*.ans's {DATE:16} column has no room for a zone suffix
			"NEWFLAG": newFlag,
		}
		b.WriteString(ansi.Render(tmpl, vars))
		b.WriteString(ansi.CRLF)
	}
	for i := end - scrollOffset; i < available; i++ {
		b.WriteString(ansi.CRLF)
	}

	scrollStatus := ""
	if len(msgs) > available {
		scrollStatus = "-- " + term.T("list.range", "FROM", scrollOffset+1, "TO", end, "TOTAL", len(msgs)) + " --"
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + scrollStatus + ansi.Reset + ansi.CRLF)
	b.WriteString(ansi.FG(ansi.White, true) + term.T("netmail.list_keys") + ansi.Reset)
	return term.Print(b.String())
}

// Fixed filenames for the hand-designed pieces of the netmail reader,
// mirroring messages.go's msgread.ans/msgread-meta.ans/
// msgread-footer.ans.
const (
	netmailReadScreen       = "netread.ans"
	netmailReadMetaScreen   = "netread-meta.ans"
	netmailReadFooterScreen = "netread-footer.ans"
)

var fallbackNetmailReadMeta = "\x1b[1;35m{T:msg.from:-9}\x1b[1;37m{FROM:-40}\x1b[1;35m {T:msg.date} \x1b[1;37m{DATE}\r\n" +
	"\x1b[1;35m{T:msg.to:-9}\x1b[1;37m{TO:-40}\r\n" +
	"\x1b[1;35m{T:msg.subject:-9}\x1b[1;37m{SUBJECT}\r\n" +
	"\x1b[35m" + strings.Repeat("-", 79) + ansi.Reset

var fallbackNetmailReadFooter = ansi.FG(ansi.White, true) + "{SCROLLSTATUS}" + ansi.Reset + "\r\n" +
	ansi.FG(ansi.White, true) + "{HINT}" + ansi.Reset

// readNetmail is a reader over msgs, starting at idx, that lets the
// caller page through their inbox with the arrow keys / N,P without
// returning to the list each time -- mirroring messages.go's
// readMessage: Up/Down scroll within the current (possibly multi-
// screen) message, clamped at its edges, while switching messages is
// only ever explicit (N/P, Left/Right, or Enter) and clamps at the
// first/last message instead of wrapping around.
func (s *Server) readNetmail(term *Terminal, u *user.User, msgs []netmail.Message, idx int) error {
	scrollOffset := 0
	for {
		if err := s.Netmail.MarkRead(msgs[idx].ID); err != nil {
			return err
		}
		maxOffset, err := s.drawNetmailReader(term, msgs, idx, scrollOffset)
		if err != nil {
			return err
		}
		key, err := term.ReadKey()
		if err != nil {
			return err
		}
		switch {
		case key.Type == KeyUp:
			if scrollOffset > 0 {
				scrollOffset--
			}
		case key.Type == KeyDown:
			if scrollOffset < maxOffset {
				scrollOffset++
			}
		case key.Type == KeyLeft, key.Type == KeyChar && (key.Rune == 'p' || key.Rune == 'P'):
			if idx > 0 {
				idx--
				scrollOffset = 0
			}
		case key.Type == KeyRight || key.Type == KeyEnter, key.Type == KeyChar && (key.Rune == 'n' || key.Rune == 'N'):
			if idx < len(msgs)-1 {
				idx++
				scrollOffset = 0
			}
		case key.Type == KeyChar && (key.Rune == 'r' || key.Rune == 'R'):
			if err := s.replyToNetmail(term, u, &msgs[idx]); err != nil {
				return err
			}
		case key.Type == KeyChar && (key.Rune == 'd' || key.Rune == 'D'):
			deleted, err := s.confirmDeleteNetmail(term, &msgs[idx])
			if err != nil {
				return err
			}
			if deleted {
				return nil
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
	// Pending: a reply to the sysop only.
	if !u.Validated && !(original.FromUserID.Valid && s.isSysop(original.FromUserID.Int64)) {
		if ok, err := s.mayPost(term, u); !ok {
			return err
		}
	}
	subject := replySubject(original.Subject)
	if err := term.Print(ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + term.T("netmail.reply_title") + ansi.Reset); err != nil {
		return err
	}
	if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + term.T("msg.to") + " " + ansi.Reset + original.FromName); err != nil {
		return err
	}
	if err := term.Println(ansi.FG(ansi.Cyan, true) + term.T("msg.subject") + " " + ansi.Reset + subject); err != nil {
		return err
	}

	lines, saved, err := s.runEditor(term, editorHeader(term, term.T("netmail.title"), original.FromName, subject),
		quoteForReply(original.Body, original.FromName, original.ToName), u.LineEditor)
	if err != nil {
		return err
	}
	if !saved {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("msg.reply_aborted"))
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
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("msg.reply_sent"))
}

// renderNetmailReaderHeader returns netread.ans (with MSGNUM/MSGCOUNT
// filled in), falling back to a plain title line -- mirrors
// messages.go's renderMessageReaderHeader, including returning a
// string (see finishHeaderLine) rather than printing directly so
// drawNetmailReader can count its line count toward the body's
// scroll viewport budget.
func (s *Server) renderNetmailReaderHeader(term *Terminal, idx, total int) string {
	raw, err := s.loadScreen(term.Lang, netmailReadScreen)
	if err != nil {
		return ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + term.T("netmail.title") + ansi.Reset + "\n"
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"MSGNUM":   strconv.Itoa(idx + 1),
		"MSGCOUNT": strconv.Itoa(total),
	}
	rendered := ansi.Render(raw, vars)
	return finishHeaderLine(ansi.Layout(rendered, term.Width()))
}

// drawNetmailReader redraws the full reader screen for msgs[idx]: the
// header banner, the From/To/Subject/Date metadata block, the body --
// windowed to whatever vertical space is left after the header/meta/
// footer, starting at scrollOffset lines in, so a long message
// scrolls within its own area instead of pushing the header off the
// top of the screen -- and a footer (its own customizable template,
// scroll status + hotkey hint) padded down to the bottom of the
// screen when the body is shorter than the viewport. Mirrors
// messages.go's drawMessageReader in every respect, ANSI-art handling
// included (see ansi.IsPreformatted/ParseGrid there for why). Returns
// maxOffset, the largest scrollOffset the caller should still accept
// for this message (0 once the whole body already fits).
func (s *Server) drawNetmailReader(term *Terminal, msgs []netmail.Message, idx, scrollOffset int) (maxOffset int, err error) {
	m := &msgs[idx]
	body := m.Body

	hint := term.T("netmail.read_keys")

	header := s.renderNetmailReaderHeader(term, idx, len(msgs))

	to := m.ToName
	if m.ToAddress != "" {
		to = fmt.Sprintf("%s (%s)", m.ToName, m.ToAddress)
	}
	metaTemplate := s.loadOptionalScreen(term, netmailReadMetaScreen, fallbackNetmailReadMeta)
	vars := ansi.Vars{
		"FROM":    m.FromName,
		"TO":      to,
		"SUBJECT": m.Subject,
		"DATE":    term.Time(m.PostedAt).Format("2006-01-02 15:04 MST"),
	}
	meta := ansi.Layout(ansi.Render(metaTemplate, vars), term.Width())
	footerTemplate := s.loadOptionalScreen(term, netmailReadFooterScreen, fallbackNetmailReadFooter)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString(ansi.Reset)
	b.WriteString(meta)
	b.WriteString(ansi.CRLF)

	used := strings.Count(header, "\n") + strings.Count(meta, "\n") + 1 + strings.Count(footerTemplate, "\n") + 1 + 1
	available := term.Height() - used
	if available < 1 {
		available = 1
	}

	preformatted := ansi.IsPreformatted(body)
	var totalLines int
	var lines []string
	var grid ansi.Grid
	if preformatted {
		grid = ansi.ParseGrid(body, term.Width())
		totalLines = grid.Height
	} else {
		lines = ansi.WrapText(body, term.Width())
		totalLines = len(lines)
	}

	maxOffset = totalLines - available
	if maxOffset < 0 {
		maxOffset = 0
	}
	if scrollOffset > maxOffset {
		scrollOffset = maxOffset
	}
	end := scrollOffset + available
	if end > totalLines {
		end = totalLines
	}

	if preformatted {
		b.WriteString(grid.EncodeRows(scrollOffset, end))
		b.WriteString(ansi.CRLF)
	} else {
		for _, line := range lines[scrollOffset:end] {
			b.WriteString(ansi.Reset + line + ansi.CRLF)
		}
	}
	for i := end - scrollOffset; i < available; i++ {
		b.WriteString(ansi.CRLF)
	}

	scrollStatus := ""
	if maxOffset > 0 {
		scrollStatus = "-- " + term.T("read.lines", "FROM", scrollOffset+1, "TO", end, "TOTAL", totalLines) + " --"
	}
	footer := ansi.Render(footerTemplate, ansi.Vars{"SCROLLSTATUS": scrollStatus, "HINT": hint})
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(footer)
	return maxOffset, term.Print(b.String())
}

// composeNetmail prompts for a recipient (an existing local username,
// or an FTN address for a system this BBS can't reach without a
// BinkP mailer yet -- see internal/netmail's doc comment), and for an
// FTN address also the recipient's name (a node has many possible
// recipients; the address alone doesn't say who a remote sysop should
// hand the message to), then a Subject, then hands off to the shared
// runLineEditor for the body.
func (s *Server) composeNetmail(term *Terminal, u *user.User) error {
	if err := term.Print(ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Magenta, true) + term.T("netmail.compose_title") + ansi.Reset); err != nil {
		return err
	}
	if err := term.Print(ansi.Reset + "\n" + term.T("netmail.to_prompt") + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	to, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return term.Println(ansi.Reset + term.T("common.cancelled"))
	}

	var toUserID int64
	var toName, toAddress string
	var crash bool
	recipient, err := s.Users.ByUsername(to)
	if !u.Validated && (err != nil || recipient.SecurityLevel < user.SLSysop) {
		// Pending: netmail to the sysop only.
		_, err := s.mayPost(term, u)
		return err
	}
	if err == nil {
		toUserID = recipient.ID
		toName = recipient.Username
	} else if !errors.Is(err, user.ErrNotFound) {
		return err
	} else if isFTNAddress(to) {
		toAddress = to
		// Who's there, by the nodelist: their sysop is the likely
		// recipient.
		defaultName := to
		if s.Nodelist != nil {
			if e, ok, _ := s.Nodelist.LookupAddress(to); ok {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "  -> " + toCP437(e.Name) + ", " +
					toCP437(e.Location) + " (" + term.T("netmail.sysop_of", "SYSOP", toCP437(e.Sysop)) + ")" + ansi.Reset); err != nil {
					return err
				}
				defaultName = toCP437(e.Sysop)
			} else if err := term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) + "  " + term.T("netmail.not_in_nodelist") + ansi.Reset); err != nil {
				return err
			}
		}
		if err := term.Print(ansi.Reset + term.T("netmail.recipient", "DEFAULT", defaultName) + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		name, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		toName = strings.TrimSpace(name)
		if toName == "" {
			toName = defaultName
		}
		if err := term.Print(ansi.Reset + term.T("netmail.crash") + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		crashAnswer, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		crashAnswer = strings.ToUpper(strings.TrimSpace(crashAnswer))
		crash = isYes(term, crashAnswer) || crashAnswer == "YES"
	} else {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) +
			term.T("netmail.bad_recipient", "TO", to))
	}

	if err := term.Print(ansi.Reset + term.T("msg.subject") + " " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	subject, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return term.Println(ansi.Reset + term.T("common.cancelled"))
	}

	label := toName
	if toAddress != "" {
		label += " (" + toAddress + ")"
	}
	lines, saved, err := s.runEditor(term, editorHeader(term, term.T("netmail.title"), label, subject), nil, u.LineEditor)
	if err != nil {
		return err
	}
	if !saved {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("msg.aborted"))
	}

	if _, err := s.Netmail.Send(u.ID, s.FTNAddress, toUserID, toName, toAddress, subject, strings.Join(lines, "\n"), crash); err != nil {
		return err
	}
	if toUserID > 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("netmail.sent"))
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) +
		term.T("netmail.queued_no_mailer", "NAME", toName, "ADDRESS", toAddress))
}
