package bbs

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// showAreas is the "builtin:areas" command: a lightbar over every
// message area the caller can read, showing each area's Total/New/
// Yours message counts and letting them move the highlighted row
// with the arrow keys, Enter to browse that area, Q/Escape to
// return.
func (s *Server) showAreas(term *Terminal, u *user.User) error {
	selected := 0
outer:
	for {
		stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
		if err != nil {
			return err
		}
		if len(stats) == 0 {
			if err := s.printAreaHeader(term, u, "msgareas.ans", "Message Areas"); err != nil {
				return err
			}
			return term.Println(ansi.Reset + "\nNo message areas available.")
		}
		if selected >= len(stats) {
			selected = len(stats) - 1
		}

		for {
			if err := s.drawAreaLightbar(term, u, stats, selected); err != nil {
				return err
			}
			key, err := term.ReadKey()
			if err != nil {
				return err
			}
			switch {
			case key.Type == KeyUp:
				selected = (selected - 1 + len(stats)) % len(stats)
			case key.Type == KeyDown:
				selected = (selected + 1) % len(stats)
			case key.Type == KeyEnter:
				area := stats[selected].Area
				if err := s.browseArea(term, u, &area); err != nil {
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

// Fixed filenames for the hand-designed pieces of the area lightbar,
// beyond the banner (see printAreaHeader): a plain, uncustomized
// fallback covers each one so a missing or deleted file degrades to
// a working (if plain) table instead of breaking the listing.
const (
	msgAreaColumnsScreen     = "msgareas-columns.ans"
	msgAreaRowScreen         = "msgareas-row.ans"
	msgAreaRowSelectedScreen = "msgareas-row-selected.ans"
)

const (
	fallbackAreaRow         = "{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}"
	fallbackAreaRowSelected = "\x1b[47m\x1b[30m{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}\x1b[0m"
)

var fallbackAreaColumns = "Area                                                           Total    New  Yours\r\n" + strings.Repeat("-", 79)

// loadOptionalScreen returns screenFile's raw content from ScreensDir,
// or fallback if it doesn't exist / can't be read.
func (s *Server) loadOptionalScreen(screenFile, fallback string) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, screenFile))
	if err != nil {
		return fallback
	}
	return raw
}

// drawAreaLightbar redraws the message-area header banner plus the
// Total/New/Yours table, with the row at selected highlighted. The
// column header and the two row styles (normal/selected) are each
// their own hand-designed screen file, rendered with the AREANAME/
// TOTAL/NEW/YOURS/NEWFLAG placeholders -- see internal/ansi.Render's
// {NAME:WIDTH} form, which is what keeps the numeric columns aligned
// even though area names vary in length.
func (s *Server) drawAreaLightbar(term *Terminal, u *user.User, stats []message.AreaWithStats, selected int) error {
	if err := s.printAreaHeader(term, u, "msgareas.ans", "Message Areas"); err != nil {
		return err
	}

	rowTemplate := s.loadOptionalScreen(msgAreaRowScreen, fallbackAreaRow)
	rowSelectedTemplate := s.loadOptionalScreen(msgAreaRowSelectedScreen, fallbackAreaRowSelected)

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(s.loadOptionalScreen(msgAreaColumnsScreen, fallbackAreaColumns))
	b.WriteString(ansi.CRLF)

	for i, st := range stats {
		tmpl := rowTemplate
		if i == selected {
			tmpl = rowSelectedTemplate
		}
		newFlag := ""
		if st.New > 0 {
			newFlag = "NEW"
		}
		vars := ansi.Vars{
			"AREANAME": st.Area.Name,
			"TOTAL":    strconv.Itoa(st.Total),
			"NEW":      strconv.Itoa(st.New),
			"YOURS":    strconv.Itoa(st.Yours),
			"NEWFLAG":  newFlag,
		}
		b.WriteString(ansi.Render(tmpl, vars))
		b.WriteString(ansi.CRLF)
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + "[Up/Down] Move   [Enter] Select   [Q] Back" + ansi.Reset)
	return term.Print(b.String())
}

// msgListScreen is the hand-designed banner shown above an area's
// message list, the same clear-screen-then-banner convention as
// printAreaHeader/printMessageReaderHeader -- without it, entering an
// area left the previous screen (e.g. the area lightbar's box) on
// screen with the list appended below it instead of replacing it.
const msgListScreen = "msglist.ans"

// printMessageListHeader shows msglist.ans (with AREANAME filled in),
// falling back to a plain colored area-name line on a cleared screen.
func (s *Server) printMessageListHeader(term *Terminal, area *message.Area) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, msgListScreen))
	if err != nil {
		return term.Println(ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset)
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"AREANAME": area.Name,
	}
	rendered := ansi.Render(raw, vars)
	return term.Println(ansi.Layout(rendered, term.Width()))
}

// Fixed filenames for the hand-designed pieces of the message-list
// lightbar, mirroring the area lightbar's msgareas-columns.ans/
// -row.ans/-row-selected.ans -- see that doc comment for why these
// are separate, customizable screen files with a plain fallback.
const (
	msgListColumnsScreen     = "msglist-columns.ans"
	msgListRowScreen         = "msglist-row.ans"
	msgListRowSelectedScreen = "msglist-row-selected.ans"
)

const (
	fallbackMsgListRow         = "\x1b[1;33m{NEWFLAG:-3} \x1b[0m{SUBJECT:-39} {FROM:-18} {DATE:16}"
	fallbackMsgListRowSelected = "\x1b[47m\x1b[30m{NEWFLAG:-3} {SUBJECT:-39} {FROM:-18} {DATE:16}\x1b[0m"
)

var fallbackMsgListColumns = "    Subject                                 From                           Date\r\n" + strings.Repeat("-", 79)

// browseArea is a lightbar over an area's messages -- the same
// interaction as showAreas over areas: arrow keys move the highlight,
// Enter opens the message reader at that message, P posts a new
// message (if the caller's SL allows), Q/Escape returns to the area
// list.
func (s *Server) browseArea(term *Terminal, u *user.User, area *message.Area) error {
	selected := 0
outer:
	for {
		msgs, err := s.Messages.ListMessages(area.ID)
		if err != nil {
			return err
		}
		readIDs, err := s.Messages.ReadMessageIDs(u.ID, area.ID)
		if err != nil {
			return err
		}
		canWrite := area.CanWrite(u.SecurityLevel)

		if len(msgs) == 0 {
			if err := s.drawEmptyMessageList(term, area, canWrite); err != nil {
				return err
			}
			key, err := term.ReadKey()
			if err != nil {
				return err
			}
			switch {
			case key.Type == KeyChar && (key.Rune == 'p' || key.Rune == 'P'):
				if err := s.attemptPostMessage(term, u, area, canWrite); err != nil {
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
			if err := s.drawMessageList(term, u, area, msgs, selected, canWrite, readIDs); err != nil {
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
				if err := s.readMessage(term, u, area, msgs, selected); err != nil {
					return err
				}
				continue outer
			case key.Type == KeyChar && (key.Rune == 'p' || key.Rune == 'P'):
				if err := s.attemptPostMessage(term, u, area, canWrite); err != nil {
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

// drawEmptyMessageList shows just the header banner and a hint bar
// for an area with no messages yet -- there's nothing to put in a
// lightbar, so this skips straight to a P/Q prompt.
func (s *Server) drawEmptyMessageList(term *Terminal, area *message.Area, canWrite bool) error {
	if err := s.printMessageListHeader(term, area); err != nil {
		return err
	}
	hint := "[Q] Back"
	if canWrite {
		hint = "[P] Post   " + hint
	}
	return term.Print(ansi.Reset + "\n(no messages yet)\r\n\r\n" + ansi.FG(ansi.White, true) + hint + ansi.Reset)
}

// drawMessageList redraws the header banner plus the Subject/From/
// Date table, with the row at selected highlighted and any message
// the caller hasn't actually opened in the reader yet (absent from
// readIDs) flagged via NEWFLAG -- the message list's equivalent of
// drawAreaLightbar. The column header and the two row styles are each
// their own hand-designed screen file so the sysop can restyle this
// table with macros/.ans files exactly like the area list.
func (s *Server) drawMessageList(term *Terminal, u *user.User, area *message.Area, msgs []message.Message, selected int, canWrite bool, readIDs map[int64]bool) error {
	if err := s.printMessageListHeader(term, area); err != nil {
		return err
	}

	rowTemplate := s.loadOptionalScreen(msgListRowScreen, fallbackMsgListRow)
	rowSelectedTemplate := s.loadOptionalScreen(msgListRowSelectedScreen, fallbackMsgListRowSelected)

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(s.loadOptionalScreen(msgListColumnsScreen, fallbackMsgListColumns))
	b.WriteString(ansi.CRLF)

	for i, m := range msgs {
		tmpl := rowTemplate
		if i == selected {
			tmpl = rowSelectedTemplate
		}
		newFlag := ""
		if !readIDs[m.ID] {
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

	hint := "[Up/Down] Move   [Enter] Read   [Q] Back"
	if canWrite {
		hint = "[Up/Down] Move   [Enter] Read   [P] Post   [Q] Back"
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + hint + ansi.Reset)
	return term.Print(b.String())
}

// Fixed filenames for the hand-designed pieces of the message reader,
// mirroring the area lightbar's screens: a full-screen header banner
// (clears the screen itself) plus a small metadata block, each with a
// plain fallback so a missing/deleted file degrades gracefully.
const (
	msgReadScreen     = "msgread.ans"
	msgReadMetaScreen = "msgread-meta.ans"
)

var fallbackMsgReadMeta = "\x1b[1;36mFrom:    \x1b[1;37m{FROM:-40}\x1b[1;36m Date: \x1b[1;37m{DATE}\r\n" +
	"\x1b[1;36mTo:      \x1b[1;37m{TO:-40}\r\n" +
	"\x1b[1;36mSubject: \x1b[1;37m{SUBJECT}\r\n" +
	"\x1b[36m" + strings.Repeat("-", 79) + ansi.Reset

// readMessage is a message reader over msgs, starting at idx, that
// lets the caller page through every message in the area with the
// arrow keys / N,P without returning to the numbered list each time
// -- Q or Escape returns to browseArea's list.
func (s *Server) readMessage(term *Terminal, u *user.User, area *message.Area, msgs []message.Message, idx int) error {
	for {
		if err := s.Messages.MarkMessageRead(u.ID, msgs[idx].ID); err != nil {
			return err
		}
		if err := s.drawMessageReader(term, u, area, msgs, idx); err != nil {
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
		case key.Type == KeyEscape:
			return nil
		case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
			return nil
		}
	}
}

// printMessageReaderHeader shows msgread.ans (with AREANAME/MSGNUM/
// MSGCOUNT filled in), falling back to a plain colored area-name line
// -- the same degrade-gracefully convention as printAreaHeader.
func (s *Server) printMessageReaderHeader(term *Terminal, area *message.Area, idx, total int) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, msgReadScreen))
	if err != nil {
		return term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset)
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"AREANAME": area.Name,
		"MSGNUM":   strconv.Itoa(idx + 1),
		"MSGCOUNT": strconv.Itoa(total),
	}
	rendered := ansi.Render(raw, vars)
	return term.Println(ansi.Layout(rendered, term.Width()))
}

// drawMessageReader redraws the full reader screen for msgs[idx]: the
// header banner, the From/To/Subject/Date metadata block (its own
// customizable screen file, like the area lightbar's column header),
// the word-wrapped body, and a footer hinting at the navigation keys.
func (s *Server) drawMessageReader(term *Terminal, u *user.User, area *message.Area, msgs []message.Message, idx int) error {
	if err := s.printMessageReaderHeader(term, area, idx, len(msgs)); err != nil {
		return err
	}
	m := &msgs[idx]

	metaTemplate := s.loadOptionalScreen(msgReadMetaScreen, fallbackMsgReadMeta)
	vars := ansi.Vars{
		"FROM":    m.FromName,
		"TO":      m.ToName,
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
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + "[Enter/Dn/Right] Next  [Up/Left] Prev  [Q] Back to list" + ansi.Reset)
	return term.Print(b.String())
}

// attemptPostMessage is the lightbar's P handler: it rejects the
// attempt with a paused message if canWrite is false (the caller's SL
// doesn't meet the area's write threshold), since the lightbar
// redraws on every keypress and would otherwise wipe the rejection
// message before it could be read -- see pauseForKey's doc comment.
func (s *Server) attemptPostMessage(term *Terminal, u *user.User, area *message.Area, canWrite bool) error {
	if !canWrite {
		if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Red, true) + "You don't have permission to post here."); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	return s.postMessage(term, u, area)
}

// msgPostScreen is the hand-designed banner shown while composing a
// new message, the same clear-screen-then-banner convention as the
// area/list/reader screens.
const msgPostScreen = "msgpost.ans"

// printPostMessageHeader shows msgpost.ans (with AREANAME filled in),
// falling back to a plain colored title line on a cleared screen.
func (s *Server) printPostMessageHeader(term *Terminal, area *message.Area) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, msgPostScreen))
	if err != nil {
		return term.Println(ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + "Post to " + area.Name + ansi.Reset)
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"AREANAME": area.Name,
	}
	rendered := ansi.Render(raw, vars)
	return term.Println(ansi.Layout(rendered, term.Width()))
}

// editorCommand identifies one of the classic BBS line-editor slash
// commands recognized by postMessage's editor loop.
type editorCommand int

const (
	editorNone editorCommand = iota
	editorSave
	editorAbort
	editorList
	editorDelete
)

// parseEditorCommand recognizes /S, /A, /L, and /D <n> case-
// insensitively; anything else is ordinary message text to append as
// a new line, matching what a Synchronet/Mystic-style message editor
// accepts.
func parseEditorCommand(line string) (cmd editorCommand, arg string) {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.EqualFold(trimmed, "/S"):
		return editorSave, ""
	case strings.EqualFold(trimmed, "/A"):
		return editorAbort, ""
	case strings.EqualFold(trimmed, "/L"):
		return editorList, ""
	case len(trimmed) >= 2 && strings.EqualFold(trimmed[:2], "/D"):
		return editorDelete, strings.TrimSpace(trimmed[2:])
	default:
		return editorNone, ""
	}
}

// printEditorHelp shows the line editor's command legend once, right
// after the Subject prompt.
func (s *Server) printEditorHelp(term *Terminal) error {
	cmd := ansi.FG(ansi.Cyan, true)
	reset := ansi.Reset
	return term.Println(reset + "\nEnter your message, one line at a time." +
		"\r\n" + cmd + "/S" + reset + " save & post   " +
		cmd + "/A" + reset + " abort   " +
		cmd + "/L" + reset + " list what you've written   " +
		cmd + "/D <n>" + reset + " delete line n")
}

// printEditorListing shows the message composed so far, numbered the
// same way as the line prompts, for the /L command.
func (s *Server) printEditorListing(term *Terminal, lines []string) error {
	if len(lines) == 0 {
		return term.Println(ansi.Reset + "\n(no lines yet)")
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	for i, line := range lines {
		fmt.Fprintf(&b, "%s%3d:%s %s\r\n", ansi.FG(ansi.Cyan, true), i+1, ansi.Reset, line)
	}
	return term.Print(b.String())
}

// postMessage is a classic BBS line editor: after a Subject prompt,
// each line of the body is entered and numbered as it's typed, with
// /S to save and post, /A to abort, /L to list what's been entered so
// far, and /D <n> to delete a line -- replacing the old bare
// type-"." -to-finish prompt with the Synchronet/Mystic-style editor
// the user asked for.
func (s *Server) postMessage(term *Terminal, u *user.User, area *message.Area) error {
	if err := s.printPostMessageHeader(term, area); err != nil {
		return err
	}
	if err := term.Print(ansi.Reset + "\nSubject: " + ansi.FG(ansi.Yellow, true)); err != nil {
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

	if err := s.printEditorHelp(term); err != nil {
		return err
	}

	var lines []string
	for {
		if err := term.Print(ansi.Reset + fmt.Sprintf("%3d: ", len(lines)+1) + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		input, err := term.ReadLine(false)
		if err != nil {
			return err
		}

		cmd, arg := parseEditorCommand(input)
		switch cmd {
		case editorSave:
			if len(lines) == 0 {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Message is empty; nothing to save."); err != nil {
					return err
				}
				continue
			}
			if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", subject, strings.Join(lines, "\n")); err != nil {
				return err
			}
			return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Message posted.")

		case editorAbort:
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Message aborted.")

		case editorList:
			if err := s.printEditorListing(term, lines); err != nil {
				return err
			}

		case editorDelete:
			idx, convErr := strconv.Atoi(arg)
			if convErr != nil || idx < 1 || idx > len(lines) {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "No such line."); err != nil {
					return err
				}
				continue
			}
			lines = append(lines[:idx-1], lines[idx:]...)
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Line %d deleted.", idx)); err != nil {
				return err
			}

		default:
			lines = append(lines, input)
		}
	}
}

// sysopCreateArea is the "builtin:createarea" command: it prompts for
// a new area's tag, name, description, and SL gates.
func (s *Server) sysopCreateArea(term *Terminal, sysop *user.User) error {
	if err := term.Print(ansi.Reset + "\nArea tag (short, no spaces): " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	tag, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	if err := term.Print(ansi.Reset + "Area name: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	name, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	if err := term.Print(ansi.Reset + "Description: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	description, err := term.ReadLine(false)
	if err != nil {
		return err
	}

	minRead, err := s.promptSecurityLevel(term, "Minimum SL to read (0-255): ")
	if err != nil {
		return err
	}
	if minRead < 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid security level.")
	}

	minWrite, err := s.promptSecurityLevel(term, "Minimum SL to post (0-255): ")
	if err != nil {
		return err
	}
	if minWrite < 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid security level.")
	}

	area, err := s.Messages.CreateArea(tag, name, description, minRead, minWrite)
	if err != nil {
		if errors.Is(err, message.ErrTagTaken) {
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "That tag is already in use.")
		}
		return err
	}
	s.logInfo("%s created message area %q (%s)", sysop.Username, area.Name, area.Tag)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Area %q created.", area.Name))
}

// promptSecurityLevel reads a 0-255 security level, returning -1 for
// any invalid or out-of-range input rather than an error, since that's
// treated as sysop input to reject, not a session-ending failure.
func (s *Server) promptSecurityLevel(term *Terminal, prompt string) (int, error) {
	if err := term.Print(ansi.Reset + prompt + ansi.FG(ansi.Yellow, true)); err != nil {
		return -1, err
	}
	input, err := term.ReadLine(false)
	if err != nil {
		return -1, err
	}
	level, convErr := strconv.Atoi(strings.TrimSpace(input))
	if convErr != nil || level < 0 || level > 255 {
		return -1, nil
	}
	return level, nil
}
