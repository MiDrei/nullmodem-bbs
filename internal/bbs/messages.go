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
			header := s.renderAreaHeader(term, u, "msgareas.ans", "Message Areas")
			return term.Print(header + ansi.Reset + "No message areas available." + ansi.CRLF)
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
// beyond the banner (see renderAreaHeader): a plain, uncustomized
// fallback covers each one so a missing or deleted file degrades to
// a working (if plain) table instead of breaking the listing.
const (
	msgAreaColumnsScreen     = "msgareas-columns.ans"
	msgAreaRowScreen         = "msgareas-row.ans"
	msgAreaRowSelectedScreen = "msgareas-row-selected.ans"
	// msgAreaNetworkScreen is the divider shown before the first area
	// of each network group (see drawAreaLightbar) -- not shown at
	// all for local/ungrouped areas (empty Network).
	msgAreaNetworkScreen = "msgareas-network.ans"
)

const (
	fallbackAreaRow         = "{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}"
	fallbackAreaRowSelected = "\x1b[47m\x1b[30m{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}\x1b[0m"
	fallbackAreaNetwork     = "\x1b[1;35m-- {NETWORK} {FILL:-}\x1b[0m"
)

var fallbackAreaColumns = "Area                                                           Total    New  Yours\r\n" + strings.Repeat("-", 79)

// loadOptionalScreen returns screenFile's raw content from ScreensDir,
// or fallback if it doesn't exist / can't be read. Every caller uses
// this for a composable FRAGMENT (a row/columns/network/meta/footer
// template meant to be printed right after a full-screen header
// banner) rather than a standalone screen, so any leading screen-
// clear the file itself contains -- e.g. left over from being saved
// in the web ANSI designer, which defaults to one -- is stripped (see
// ansi.StripLeadingScreenClear): otherwise it would wipe out the
// header banner drawn just before it.
func (s *Server) loadOptionalScreen(screenFile, fallback string) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, screenFile))
	if err != nil {
		return fallback
	}
	return ansi.StripLeadingScreenClear(raw)
}

// areaDisplayRow is one line drawAreaLightbar/drawFileAreaLightbar
// will actually put on screen: either a network divider or one area's
// row. Windowing over a list of these (rather than over the area
// stats directly) is what keeps a divider from silently eating an
// extra row the viewport budget didn't account for.
type areaDisplayRow struct {
	divider  string
	statsIdx int
	isArea   bool
}

// buildAreaDisplayRows turns stats (sorted network, sort_order, name
// -- see ListAreaStats/file.Store's equivalent, so every area sharing
// a network is already contiguous) into rows, inserting a divider
// right before the first area of each new, non-empty network group;
// local/ungrouped areas (Network == "") never get one. Also returns
// which row index selected landed on, so the caller can scroll it
// into view.
func buildAreaDisplayRows(stats []message.AreaWithStats, selected int, networkTemplate string, width int) (rows []areaDisplayRow, selectedRow int) {
	lastNetwork := ""
	for i, st := range stats {
		if st.Area.Network != lastNetwork {
			if st.Area.Network != "" {
				rows = append(rows, areaDisplayRow{divider: ansi.Layout(ansi.Render(networkTemplate, ansi.Vars{"NETWORK": st.Area.Network}), width)})
			}
			lastNetwork = st.Area.Network
		}
		rows = append(rows, areaDisplayRow{statsIdx: i, isArea: true})
		if i == selected {
			selectedRow = len(rows) - 1
		}
	}
	return rows, selectedRow
}

// drawAreaLightbar redraws the message-area header banner plus the
// Total/New/Yours table, with the row at selected highlighted. The
// column header and the two row styles (normal/selected) are each
// their own hand-designed screen file, rendered with the AREANAME/
// TOTAL/NEW/YOURS/NEWFLAG placeholders -- see internal/ansi.Render's
// {NAME:WIDTH} form, which is what keeps the numeric columns aligned
// even though area names vary in length.
//
// Like drawMessageList, the table is windowed to whatever vertical
// space is left after the header/columns/footer, scrolled to keep
// selected in view -- a long area list (routine now that fsxNet/
// HobbyNet/lovlynet echomail areas get auto-created as they're
// tossed in) used to just dump every row in one shot, pushing the
// header off the top of the screen exactly the way an unpaginated
// message list once did.
func (s *Server) drawAreaLightbar(term *Terminal, u *user.User, stats []message.AreaWithStats, selected int) error {
	header := s.renderAreaHeader(term, u, "msgareas.ans", "Message Areas")

	rowTemplate := s.loadOptionalScreen(msgAreaRowScreen, fallbackAreaRow)
	rowSelectedTemplate := s.loadOptionalScreen(msgAreaRowSelectedScreen, fallbackAreaRowSelected)
	networkTemplate := s.loadOptionalScreen(msgAreaNetworkScreen, fallbackAreaNetwork)
	columns := s.loadOptionalScreen(msgAreaColumnsScreen, fallbackAreaColumns)

	rows, selectedRow := buildAreaDisplayRows(stats, selected, networkTemplate, term.Width())

	var b strings.Builder
	b.WriteString(header)
	b.WriteString(ansi.Reset)
	b.WriteString(columns)
	b.WriteString(ansi.CRLF)

	// Budget the table's viewport the same way drawMessageList does:
	// header/columns counted from their own rendered text (deployment-
	// customizable, not a fixed line count; header's own trailing "\n"
	// -- see finishHeaderLine -- already accounts for its own last
	// row, with no separator row of ours added on top) plus the
	// footer's own fixed 3 lines (blank + its own scroll-status line,
	// always reserved even when blank, plus the hint line).
	used := strings.Count(header, "\n") + strings.Count(columns, "\n") + 1 + 3
	available := term.Height() - used
	if available < 1 {
		available = 1
	}

	scrollOffset := selectedRow - available/2
	if scrollOffset > len(rows)-available {
		scrollOffset = len(rows) - available
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	end := scrollOffset + available
	if end > len(rows) {
		end = len(rows)
	}

	for i := scrollOffset; i < end; i++ {
		row := rows[i]
		if !row.isArea {
			b.WriteString(row.divider)
			b.WriteString(ansi.CRLF)
			continue
		}
		st := stats[row.statsIdx]
		tmpl := rowTemplate
		if row.statsIdx == selected {
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
	for i := end - scrollOffset; i < available; i++ {
		b.WriteString(ansi.CRLF)
	}

	scrollStatus := ""
	if len(rows) > available {
		scrollStatus = fmt.Sprintf("-- %d-%d of %d --", scrollOffset+1, end, len(rows))
	}
	b.WriteString(ansi.Reset + ansi.CRLF + ansi.FG(ansi.White, true) + scrollStatus + ansi.Reset + ansi.CRLF)
	b.WriteString(ansi.FG(ansi.White, true) + "[Up/Down] Move   [Enter] Select   [Q] Back" + ansi.Reset)
	return term.Print(b.String())
}

// msgListScreen is the hand-designed banner shown above an area's
// message list, the same clear-screen-then-banner convention as
// renderAreaHeader/renderMessageReaderHeader -- without it, entering an
// area left the previous screen (e.g. the area lightbar's box) on
// screen with the list appended below it instead of replacing it.
const msgListScreen = "msglist.ans"

// renderMessageListHeader returns msglist.ans (with AREANAME filled
// in) followed by a newline, falling back to a plain colored area-
// name line on a cleared screen. Returned as a string rather than
// printed directly so drawMessageList can count its line count
// (customizable per deployment, so not something to hardcode) toward
// the list's scroll viewport budget -- mirrors
// renderMessageReaderHeader for the same reason.
func (s *Server) renderMessageListHeader(term *Terminal, area *message.Area) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, msgListScreen))
	if err != nil {
		return ansi.ClearScreen() + ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset + "\n"
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"AREANAME": area.Name,
	}
	rendered := ansi.Render(raw, vars)
	return finishHeaderLine(ansi.Layout(rendered, term.Width()))
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
	hint := "[Q] Back"
	if canWrite {
		hint = "[P] Post   " + hint
	}
	return term.Print(s.renderMessageListHeader(term, area) + ansi.Reset + "(no messages yet)\r\n\r\n" + ansi.FG(ansi.White, true) + hint + ansi.Reset)
}

// drawMessageList redraws the header banner plus the Subject/From/
// Date table, with the row at selected highlighted and any message
// the caller hasn't actually opened in the reader yet (absent from
// readIDs) flagged via NEWFLAG -- the message list's equivalent of
// drawAreaLightbar. The column header and the two row styles are each
// their own hand-designed screen file so the sysop can restyle this
// table with macros/.ans files exactly like the area list.
//
// Like drawMessageReader, the row table is windowed to whatever
// vertical space is left after the header/columns/footer, keeping
// selected in view (scrolled to try to center it, recomputed fresh
// from selected every redraw rather than tracked as separate state,
// since Up/Down here always moves the selection by exactly one row
// unlike the reader's independent body scroll) -- a long list used to
// just dump every row in one shot, pushing the header off the top of
// the screen exactly the way an unpaginated message body once did.
// Fewer rows than fit on screen are padded with blank lines so the
// footer always lands on the same line regardless of how many
// messages there are, instead of trailing right after the last one.
func (s *Server) drawMessageList(term *Terminal, u *user.User, area *message.Area, msgs []message.Message, selected int, canWrite bool, readIDs map[int64]bool) error {
	header := s.renderMessageListHeader(term, area)

	rowTemplate := s.loadOptionalScreen(msgListRowScreen, fallbackMsgListRow)
	rowSelectedTemplate := s.loadOptionalScreen(msgListRowSelectedScreen, fallbackMsgListRowSelected)
	columns := s.loadOptionalScreen(msgListColumnsScreen, fallbackMsgListColumns)

	hint := "[Up/Down] Move   [Enter] Read   [Q] Back"
	if canWrite {
		hint = "[Up/Down] Move   [Enter] Read   [P] Post   [Q] Back"
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString(ansi.Reset)
	b.WriteString(columns)
	b.WriteString(ansi.CRLF)

	// Budget the list's viewport as whatever's left after everything
	// else on screen: header/columns (counted from their own rendered
	// text, since msglist.ans/msglist-columns.ans are deployment-
	// customizable, not a fixed line count; header's own trailing "\n"
	// -- see finishHeaderLine -- already accounts for its own last
	// row, with no separator row of ours added on top) plus the
	// footer's own fixed 3 lines (blank + its own scroll-status line,
	// always reserved even when blank -- see drawMessageReader's
	// identically motivated budget -- plus the hint line).
	used := strings.Count(header, "\n") + strings.Count(columns, "\n") + 1 + 3
	available := term.Height() - used
	if available < 1 {
		available = 1
	}

	scrollOffset := selected - available/2
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
	for i := end - scrollOffset; i < available; i++ {
		b.WriteString(ansi.CRLF)
	}

	scrollStatus := ""
	if len(msgs) > available {
		scrollStatus = fmt.Sprintf("-- %d-%d of %d --", scrollOffset+1, end, len(msgs))
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + scrollStatus + ansi.Reset + ansi.CRLF)
	b.WriteString(ansi.FG(ansi.White, true) + hint + ansi.Reset)
	return term.Print(b.String())
}

// Fixed filenames for the hand-designed pieces of the message reader,
// mirroring the area lightbar's screens: a full-screen header banner
// (clears the screen itself), a small metadata block, and the footer
// bar (scroll status + hotkey hint, filled in via {SCROLLSTATUS}/
// {HINT} -- see drawMessageReader), each with a plain fallback so a
// missing/deleted file degrades gracefully.
const (
	msgReadScreen       = "msgread.ans"
	msgReadMetaScreen   = "msgread-meta.ans"
	msgReadFooterScreen = "msgread-footer.ans"
)

var fallbackMsgReadMeta = "\x1b[1;36mFrom:    \x1b[1;37m{FROM:-40}\x1b[1;36m Date: \x1b[1;37m{DATE}\r\n" +
	"\x1b[1;36mTo:      \x1b[1;37m{TO:-40}\r\n" +
	"\x1b[1;36mSubject: \x1b[1;37m{SUBJECT}\r\n" +
	"\x1b[36m" + strings.Repeat("-", 79) + ansi.Reset

var fallbackMsgReadFooter = ansi.FG(ansi.White, true) + "{SCROLLSTATUS}" + ansi.Reset + "\r\n" +
	ansi.FG(ansi.White, true) + "{HINT}" + ansi.Reset

// readMessage is a message reader over msgs, starting at idx, that
// lets the caller page through every message in the area with the
// arrow keys / N,P without returning to the numbered list each time
// -- Q or Escape returns to browseArea's list.
func (s *Server) readMessage(term *Terminal, u *user.User, area *message.Area, msgs []message.Message, idx int) error {
	// scrollOffset is how far into the current message's (possibly
	// multi-screen) body the visible window starts -- reset to 0
	// whenever idx changes, since a newly opened message always
	// starts at its own top. Up/Down scroll within it, clamped at the
	// edges (see drawMessageReader's maxOffset); switching messages is
	// only ever explicit (N/P, Left/Right, or Enter), never a side
	// effect of scrolling past an edge.
	scrollOffset := 0
	for {
		if err := s.Messages.MarkMessageRead(u.ID, msgs[idx].ID); err != nil {
			return err
		}
		maxOffset, err := s.drawMessageReader(term, u, area, msgs, idx, scrollOffset)
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
			if err := s.replyToMessage(term, u, area, &msgs[idx]); err != nil {
				return err
			}
		case key.Type == KeyEscape:
			return nil
		case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
			return nil
		}
	}
}

// renderMessageReaderHeader returns msgread.ans (with AREANAME/MSGNUM/
// MSGCOUNT filled in), followed by a newline, falling back to a plain
// colored area-name line -- the same degrade-gracefully convention as
// renderAreaHeader. Returned as a string rather than printed directly
// so drawMessageReader can count its line count (customizable per
// deployment, so not something to hardcode) toward the body's scroll
// viewport budget.
func (s *Server) renderMessageReaderHeader(term *Terminal, area *message.Area, idx, total int) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, msgReadScreen))
	if err != nil {
		return ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset + "\n"
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"AREANAME": area.Name,
		"MSGNUM":   strconv.Itoa(idx + 1),
		"MSGCOUNT": strconv.Itoa(total),
	}
	rendered := ansi.Render(raw, vars)
	return finishHeaderLine(ansi.Layout(rendered, term.Width()))
}

// printBody finishes a reader/description display: it prints
// everything already accumulated in b (header/meta), then the body,
// then footer. Used for message/netmail bodies and file descriptions
// alike, so an ANSI-tagged ad area (or a file description someone
// pasted ANSI art into) displays correctly wherever it shows up, not
// just in one of them.
//
// Ordinary prose is word-wrapped to width and joins b/footer in one
// combined Print call, same as before.
//
// Pre-formatted content (see ansi.IsPreformatted -- real ANSI escape
// codes, or plain CP437 block/line-drawing art with none at all)
// takes a completely different path: b is discarded (not an oversight
// -- see below) and body is run through ansi.ParseGrid/Grid.Encode,
// the same off-screen-canvas resolution the web ANSI designer uses to
// load a .ans file, before being printed. Three independent things go
// wrong otherwise, all confirmed live against real fsxNet ads:
// reflowing it via WrapText treats every escape-sequence byte as an
// ordinary character (corrupting the color codes) and collapses
// deliberate runs of spaces in plain block art with no color codes at
// all; even sending its bytes through completely untouched (which
// avoids the first problem) still comes out scrambled, because real
// ANSI art positions itself with absolute/relative cursor moves that
// assume they're starting at a blank screen's row 1 col 1 -- exactly
// what b (already printed above the body) makes false. ParseGrid
// resolves every position (and cursor save/restore) against a
// virtual canvas at parse time; Encode then serializes the result as
// a plain top-to-bottom character+color stream with no positioning
// codes left to misinterpret, and its own leading clear+home is what
// makes discarding b correct -- the art already intends to own the
// whole screen, exactly like a real BBS reader's full-screen ANSI ad.
func printBody(term *Terminal, b *strings.Builder, body, footer string, width int) error {
	if ansi.IsPreformatted(body) {
		grid := ansi.ParseGrid(body, width)
		if err := term.PrintRaw(grid.Encode()); err != nil {
			return err
		}
		return term.Print(ansi.CRLF + footer)
	}
	for _, line := range ansi.WrapText(body, width) {
		b.WriteString(ansi.Reset + line + ansi.CRLF)
	}
	b.WriteString(footer)
	return term.Print(b.String())
}

// stripSeenByAndPathForDisplay hides trailing SEEN-BY and PATH lines
// (FTS-0004 echomail routing/dupe-detection metadata that every
// tosser along the way appends -- a real message can carry a dozen or
// more SEEN-BY lines) from the reader. Unlike those, the tearline
// ("--- ...") and origin line ("* Origin: ...") directly above them
// are left alone: real BBS software shows those as the message's
// visible attribution footer, only SEEN-BY/PATH are meant for
// tossers, never readers. Also absorbs any blank line left dangling
// between that footer and the hidden block. A body with neither is
// returned unchanged.
//
// This only affects display -- internal/tosser stores the full body,
// SEEN-BY/PATH included, so a sysop tracing a routing/dupe problem
// can still get at it (e.g. straight from the database) rather than
// it being destroyed the moment a message is tossed.
func stripSeenByAndPathForDisplay(body string) string {
	lines := strings.Split(body, "\n")
	end := len(lines)
	for end > 0 {
		line := strings.TrimSpace(lines[end-1])
		if line == "" {
			end--
			continue
		}
		// PATH is commonly \x01-kludged even though SEEN-BY isn't --
		// strip that leading control byte before matching the prefix,
		// or it never matches and the whole block (SEEN-BY lines
		// included, since the scan works backward and stops at PATH)
		// is left showing.
		upper := strings.ToUpper(strings.TrimPrefix(line, "\x01"))
		if strings.HasPrefix(upper, "SEEN-BY:") || strings.HasPrefix(upper, "PATH:") {
			end--
			continue
		}
		break
	}
	return strings.Join(lines[:end], "\n")
}

// drawMessageReader redraws the full reader screen for msgs[idx]: the
// header banner, the From/To/Subject/Date metadata block (its own
// customizable screen file, like the area lightbar's column header),
// the body -- windowed to whatever vertical space is left after the
// header/meta/footer, starting at scrollOffset lines in, so a long
// message scrolls within its own area instead of pushing the header
// off the top of the screen -- and a footer hinting at the navigation
// keys. Returns maxOffset, the largest scrollOffset the caller should
// still accept for this message (0 once the whole body already fits).
//
// Pre-formatted content (see ansi.IsPreformatted -- real ANSI escape
// codes, or plain CP437 block/line-drawing art with none at all) is
// resolved against a virtual canvas first (ansi.ParseGrid/Grid.
// EncodeRows, the same mechanism the web ANSI designer uses to load a
// .ans file) instead of being word-wrapped -- see printBody's doc
// comment for why that corrupts it. A resolved grid's rows can then
// be windowed exactly like ordinary text's lines for scrolling, and
// -- unlike the art's own original bytes -- no longer depend on where
// on the real screen they land, so the header/meta banner drawn above
// them no longer misaligns anything, and scrolling works the same
// way it does for plain text.
func (s *Server) drawMessageReader(term *Terminal, u *user.User, area *message.Area, msgs []message.Message, idx, scrollOffset int) (maxOffset int, err error) {
	m := &msgs[idx]
	body := stripSeenByAndPathForDisplay(m.Body)

	hint := "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [Q] Back to list"
	if area.CanWrite(u.SecurityLevel) {
		hint = "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [R] Reply  [Q] Back to list"
	}

	header := s.renderMessageReaderHeader(term, area, idx, len(msgs))

	metaTemplate := s.loadOptionalScreen(msgReadMetaScreen, fallbackMsgReadMeta)
	vars := ansi.Vars{
		"FROM":    m.FromName,
		"TO":      m.ToName,
		"SUBJECT": m.Subject,
		"DATE":    m.PostedAt.Format("2006-01-02 15:04"),
	}
	meta := ansi.Layout(ansi.Render(metaTemplate, vars), term.Width())
	footerTemplate := s.loadOptionalScreen(msgReadFooterScreen, fallbackMsgReadFooter)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString(ansi.Reset)
	b.WriteString(meta)
	b.WriteString(ansi.CRLF)

	// Budget the body's viewport as whatever's left after everything
	// else drawn on screen: header/meta/footer counted from their own
	// rendered text (msgread.ans/msgread-meta.ans/msgread-footer.ans
	// are all deployment-customizable, not a fixed line count -- see
	// finishHeaderLine for why header's own trailing "\n" already
	// accounts for its own last row with no separator row of ours
	// added on top) plus one always-reserved blank line ahead of the
	// footer, so a long line-count doesn't wrap the hotkey hint onto a
	// second physical row -- that happened for real once the scroll-
	// status text was appended directly onto the hint line instead,
	// silently eating one more row than budgeted and pushing the
	// header off the top.
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
	// Pad with blank lines when the body is shorter than the viewport
	// -- otherwise the footer trails right after a short message
	// instead of staying anchored near the bottom of the screen,
	// mirroring drawMessageList/drawAreaLightbar's identically
	// motivated padding.
	for i := end - scrollOffset; i < available; i++ {
		b.WriteString(ansi.CRLF)
	}

	scrollStatus := ""
	if maxOffset > 0 {
		scrollStatus = fmt.Sprintf("-- line %d-%d of %d --", scrollOffset+1, end, totalLines)
	}
	footer := ansi.Render(footerTemplate, ansi.Vars{"SCROLLSTATUS": scrollStatus, "HINT": hint})
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(footer)
	return maxOffset, term.Print(b.String())
}

// replyToMessage posts a reply to original in the same area: Subject
// defaults to "Re: <original subject>" (see replySubject) and To to
// the original author's name, then hands off to the shared
// runLineEditor for the body -- the same /S /A /L /D editor postMessage
// uses, just with To/Subject prefilled instead of prompted.
func (s *Server) replyToMessage(term *Terminal, u *user.User, area *message.Area, original *message.Message) error {
	if !area.CanWrite(u.SecurityLevel) {
		if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Red, true) + "You don't have permission to post here."); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}

	subject := replySubject(original.Subject)
	if err := s.printPostMessageHeader(term, area); err != nil {
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
	if _, err := s.Messages.PostMessage(area.ID, u.ID, original.FromName, subject, strings.Join(lines, "\n")); err != nil {
		return err
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Reply posted.")
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

// postMessage prompts for a Subject, then hands off to the shared
// classic-BBS runLineEditor (see editor.go) for the body -- /S to
// save and post, /A to abort, /L to list what's been entered so far,
// /D <n> to delete a line.
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

	lines, saved, err := s.runLineEditor(term)
	if err != nil {
		return err
	}
	if !saved {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Message aborted.")
	}
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", subject, strings.Join(lines, "\n")); err != nil {
		return err
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Message posted.")
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

	if err := term.Print(ansi.Reset + "Network (optional, e.g. fsxNet, FidoNet; blank for local-only): " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	network, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	network = strings.TrimSpace(network)

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

	area, err := s.Messages.CreateArea(tag, name, description, network, minRead, minWrite)
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
