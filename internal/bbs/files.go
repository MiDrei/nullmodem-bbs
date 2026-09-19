package bbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
	"git.maik.ch/swissmaik/nullmodem/internal/zmodem"
)

// showFileAreas is the "builtin:files" command: a lightbar over every
// file area the caller can browse, showing each area's Total/New/
// Yours file counts and letting them move the highlighted row with
// the arrow keys, Enter to browse that area, Q/Escape to return --
// the same interaction internal/bbs/messages.go's showAreas uses for
// message areas.
func (s *Server) showFileAreas(term *Terminal, u *user.User) error {
	selected := 0
	scrollOffset := 0
outer:
	for {
		stats, err := s.Files.ListAreaStats(u.SecurityLevel, u.ID)
		if err != nil {
			return err
		}
		if len(stats) == 0 {
			header := s.renderAreaHeader(term, u, "filareas.ans", "File Areas")
			return term.Print(header + ansi.Reset + "No file areas available." + ansi.CRLF)
		}
		if selected >= len(stats) {
			selected = len(stats) - 1
		}

		for {
			scrollOffset, err = s.drawFileAreaLightbar(term, u, stats, selected, scrollOffset)
			if err != nil {
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
				if selected < len(stats)-1 {
					selected++
				}
			case key.Type == KeyEnter:
				area := stats[selected].Area
				if err := s.browseFileArea(term, u, &area); err != nil {
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

// Fixed filenames for the hand-designed pieces of the file-area
// lightbar, mirroring msgareas-columns.ans/-row.ans/-row-selected.ans
// in internal/bbs/messages.go -- see that file's doc comments for why
// these are separate, customizable screen files with a plain fallback.
const (
	fileAreaColumnsScreen     = "filareas-columns.ans"
	fileAreaRowScreen         = "filareas-row.ans"
	fileAreaRowSelectedScreen = "filareas-row-selected.ans"
	// fileAreaNetworkScreen is the divider shown before the first area
	// of each network group -- see messages.go's msgAreaNetworkScreen,
	// which this mirrors.
	fileAreaNetworkScreen = "filareas-network.ans"
)

const (
	fallbackFileAreaRow         = "{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}"
	fallbackFileAreaRowSelected = "\x1b[47m\x1b[30m{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}\x1b[0m"
	fallbackFileAreaNetwork     = "\x1b[1;35m-- {NETWORK} {FILL:-}\x1b[0m"
)

var fallbackFileAreaColumns = "Area                                                           Total    New  Yours\r\n" + strings.Repeat("-", 79)

// fileAreaDisplayRow mirrors messages.go's areaDisplayRow exactly,
// against file.AreaWithStats instead of message.AreaWithStats -- see
// that type's doc comment.
type fileAreaDisplayRow struct {
	divider  string
	statsIdx int
	isArea   bool
}

// buildFileAreaDisplayRows mirrors messages.go's
// buildAreaDisplayRows exactly -- see that function's doc comment.
func buildFileAreaDisplayRows(stats []file.AreaWithStats, selected int, networkTemplate string, width int) (rows []fileAreaDisplayRow, selectedRow int) {
	lastNetwork := ""
	for i, st := range stats {
		if st.Area.Network != lastNetwork {
			if st.Area.Network != "" {
				rows = append(rows, fileAreaDisplayRow{divider: ansi.Layout(ansi.Render(networkTemplate, ansi.Vars{"NETWORK": st.Area.Network}), width)})
			}
			lastNetwork = st.Area.Network
		}
		rows = append(rows, fileAreaDisplayRow{statsIdx: i, isArea: true})
		if i == selected {
			selectedRow = len(rows) - 1
		}
	}
	return rows, selectedRow
}

// drawFileAreaLightbar mirrors messages.go's drawAreaLightbar
// exactly, scrolling (caller-tracked scrollOffset that only follows
// the highlight once it reaches the viewport's edge, not recomputed
// fresh from selected every redraw) included -- against the file-area
// column/row screen files and file.AreaWithStats instead of
// message.AreaWithStats.
func (s *Server) drawFileAreaLightbar(term *Terminal, u *user.User, stats []file.AreaWithStats, selected, scrollOffset int) (int, error) {
	header := s.renderAreaHeader(term, u, "filareas.ans", "File Areas")

	rowTemplate := s.loadOptionalScreen(fileAreaRowScreen, fallbackFileAreaRow)
	rowSelectedTemplate := s.loadOptionalScreen(fileAreaRowSelectedScreen, fallbackFileAreaRowSelected)
	networkTemplate := s.loadOptionalScreen(fileAreaNetworkScreen, fallbackFileAreaNetwork)
	columns := s.loadOptionalScreen(fileAreaColumnsScreen, fallbackFileAreaColumns)

	rows, selectedRow := buildFileAreaDisplayRows(stats, selected, networkTemplate, term.Width())

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

	if selectedRow < scrollOffset {
		scrollOffset = selectedRow
	}
	if selectedRow >= scrollOffset+available {
		scrollOffset = selectedRow - available + 1
	}
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
	return scrollOffset, term.Print(b.String())
}

// fileListScreen is the hand-designed banner shown above an area's
// file list -- see messages.go's msgListScreen doc comment for why
// this clear-screen-then-banner is needed instead of printing the
// list inline over whatever screen (e.g. the area lightbar) was there
// before.
const fileListScreen = "fillist.ans"

// renderFileListHeader returns fillist.ans (with AREANAME filled in),
// falling back to a plain colored area-name line on a cleared screen
// -- mirrors messages.go's renderMessageListHeader, including
// returning a string (see finishHeaderLine) rather than printing
// directly so drawFileList can count its line count toward the list's
// scroll viewport budget.
func (s *Server) renderFileListHeader(term *Terminal, area *file.Area) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, fileListScreen))
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

// Fixed filenames for the hand-designed pieces of the file-list
// lightbar, mirroring messages.go's msglist-columns.ans/-row.ans/
// -row-selected.ans -- see that doc comment for why these are
// separate, customizable screen files with a plain fallback.
const (
	fileListColumnsScreen     = "fillist-columns.ans"
	fileListRowScreen         = "fillist-row.ans"
	fileListRowSelectedScreen = "fillist-row-selected.ans"
)

const (
	fallbackFileListRow         = "\x1b[1;33m{NEWFLAG:-3} \x1b[0m{FILENAME:-30} {BY:-16} {SIZE:10} {DATE:16}"
	fallbackFileListRowSelected = "\x1b[47m\x1b[30m{NEWFLAG:-3} {FILENAME:-30} {BY:-16} {SIZE:10} {DATE:16}\x1b[0m"
)

var fallbackFileListColumns = "    Filename                       By                     Size             Date\r\n" + strings.Repeat("-", 79)

// firstUnreadFileIndex mirrors messages.go's firstUnreadIndex exactly,
// against a file area's per-file read state instead of a message
// area's -- see that function's doc comment for the full rationale.
func firstUnreadFileIndex(files []file.File, readIDs map[int64]bool) int {
	for i, f := range files {
		if !readIDs[f.ID] {
			return i
		}
	}
	if len(files) == 0 {
		return 0
	}
	return len(files) - 1
}

// browseFileArea is a lightbar over an area's files -- the same
// interaction as messages.go's browseArea over messages: arrow keys
// move the highlight (clamped at the first/last file, not wrapping
// around), Enter opens the file reader at that file, U uploads, D
// downloads the highlighted file directly, Q/Escape returns to the
// area list. The initial selection lands on the first unread file
// (see firstUnreadFileIndex), computed once up front -- rereading it
// on every trip back from the reader would fight the caller's own
// navigation, jumping the highlight somewhere new right after they
// just viewed something. scrollOffset is tracked alongside selected
// the same way -- see drawFileList's doc comment.
func (s *Server) browseFileArea(term *Terminal, u *user.User, area *file.Area) error {
	selected := -1
	scrollOffset := 0
outer:
	for {
		files, err := s.Files.ListFiles(area.ID)
		if err != nil {
			return err
		}
		readIDs, err := s.Files.ReadFileIDs(u.ID, area.ID)
		if err != nil {
			return err
		}

		if len(files) == 0 {
			if err := s.drawEmptyFileList(term, area); err != nil {
				return err
			}
			key, err := term.ReadKey()
			if err != nil {
				return err
			}
			switch {
			case key.Type == KeyChar && (key.Rune == 'u' || key.Rune == 'U'):
				if err := s.uploadFile(term, u, area); err != nil {
					return err
				}
				continue outer
			case key.Type == KeyEscape, key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
				return nil
			}
			continue
		}
		if selected < 0 {
			selected = firstUnreadFileIndex(files, readIDs)
			scrollOffset = selected
		}
		if selected >= len(files) {
			selected = len(files) - 1
		}

		for {
			scrollOffset, err = s.drawFileList(term, area, files, selected, scrollOffset, readIDs)
			if err != nil {
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
				if selected < len(files)-1 {
					selected++
				}
			case key.Type == KeyEnter:
				if err := s.readFile(term, u, area, files, selected); err != nil {
					return err
				}
				continue outer
			case key.Type == KeyChar && (key.Rune == 'd' || key.Rune == 'D'):
				if err := s.downloadFile(term, u, &files[selected]); err != nil {
					return err
				}
				continue outer
			case key.Type == KeyChar && (key.Rune == 'u' || key.Rune == 'U'):
				if err := s.uploadFile(term, u, area); err != nil {
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

// drawEmptyFileList shows just the header banner and a hint bar for
// an area with no files yet -- see messages.go's drawEmptyMessageList.
func (s *Server) drawEmptyFileList(term *Terminal, area *file.Area) error {
	return term.Print(s.renderFileListHeader(term, area) + ansi.Reset + "(no files yet)\r\n\r\n" + ansi.FG(ansi.White, true) + "[U] Upload   [Q] Back" + ansi.Reset)
}

// drawFileList redraws the header banner plus the Filename/By/Size/
// Date table, with the row at selected highlighted and any file the
// caller hasn't actually opened in the reader yet flagged via
// NEWFLAG -- the file list's equivalent of messages.go's
// drawMessageList, scrolling included: the table is windowed to
// whatever vertical space is left after the header/columns/footer
// (a long list used to just dump every row in one shot, pushing the
// header off the top of the screen exactly the way an unpaginated
// message list once did), and padded with blank lines when there are
// fewer files than fit so the footer always lands on the same row
// instead of trailing right after the last one.
//
// scrollOffset is caller-tracked state (see browseFileArea), not
// recomputed fresh from selected every redraw -- see drawMessageList's
// identically motivated doc comment for why: the viewport only
// follows the highlight once it reaches the top/bottom edge, like a
// normal pager, instead of pinning it to a fixed screen row. The
// returned value is what the caller should pass back in on the next
// call.
func (s *Server) drawFileList(term *Terminal, area *file.Area, files []file.File, selected, scrollOffset int, readIDs map[int64]bool) (int, error) {
	header := s.renderFileListHeader(term, area)

	rowTemplate := s.loadOptionalScreen(fileListRowScreen, fallbackFileListRow)
	rowSelectedTemplate := s.loadOptionalScreen(fileListRowSelectedScreen, fallbackFileListRowSelected)
	columns := s.loadOptionalScreen(fileListColumnsScreen, fallbackFileListColumns)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString(ansi.Reset)
	b.WriteString(columns)
	b.WriteString(ansi.CRLF)

	// Budget the list's viewport the same way drawMessageList does:
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

	if selected < scrollOffset {
		scrollOffset = selected
	}
	if selected >= scrollOffset+available {
		scrollOffset = selected - available + 1
	}
	if scrollOffset > len(files)-available {
		scrollOffset = len(files) - available
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	end := scrollOffset + available
	if end > len(files) {
		end = len(files)
	}

	for i := scrollOffset; i < end; i++ {
		f := files[i]
		tmpl := rowTemplate
		if i == selected {
			tmpl = rowSelectedTemplate
		}
		newFlag := ""
		if !readIDs[f.ID] {
			newFlag = "NEW"
		}
		vars := ansi.Vars{
			"FILENAME": f.Filename,
			"BY":       f.UploadedByName,
			"SIZE":     humanize.Bytes(uint64(f.SizeBytes)),
			"DATE":     f.UploadedAt.Format("2006-01-02 15:04"),
			"NEWFLAG":  newFlag,
		}
		b.WriteString(ansi.Render(tmpl, vars))
		b.WriteString(ansi.CRLF)
	}
	for i := end - scrollOffset; i < available; i++ {
		b.WriteString(ansi.CRLF)
	}

	scrollStatus := ""
	if len(files) > available {
		scrollStatus = fmt.Sprintf("-- %d-%d of %d --", scrollOffset+1, end, len(files))
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + scrollStatus + ansi.Reset + ansi.CRLF)
	b.WriteString(ansi.FG(ansi.White, true) + "[Up/Down] Move   [Enter] View   [D] Download   [U] Upload   [Q] Back" + ansi.Reset)
	return scrollOffset, term.Print(b.String())
}

// Fixed filenames for the hand-designed pieces of the file reader,
// mirroring messages.go's msgread.ans/msgread-meta.ans/
// msgread-footer.ans: a full-screen header banner, a small metadata
// block, and the footer bar (scroll status + hotkey hint, filled in
// via {SCROLLSTATUS}/{HINT} -- see drawFileReader), each with a plain
// fallback so a missing/deleted file degrades gracefully.
const (
	fileReadScreen       = "filread.ans"
	fileReadMetaScreen   = "filread-meta.ans"
	fileReadFooterScreen = "filread-footer.ans"
)

var fallbackFileReadMeta = "\x1b[1;32mFilename:  \x1b[1;37m{FILENAME:-40}\x1b[1;32m Size: \x1b[1;37m{SIZE}\r\n" +
	"\x1b[1;32mUploaded:  \x1b[1;37m{DATE}\x1b[1;32m by \x1b[1;37m{BY}\r\n" +
	"\x1b[1;32mDownloads: \x1b[1;37m{DOWNLOADS}\r\n" +
	"\x1b[32m" + strings.Repeat("-", 79) + ansi.Reset

var fallbackFileReadFooter = ansi.FG(ansi.White, true) + "{SCROLLSTATUS}" + ansi.Reset + "\r\n" +
	ansi.FG(ansi.White, true) + "{HINT}" + ansi.Reset

// readFile is a file-details reader over files, starting at idx, that
// lets the caller page through every file in the area without
// returning to the list each time -- mirrors messages.go's
// readMessage exactly: Up/Down scroll within the current (possibly
// multi-screen) description, clamped at its edges, while switching
// files is only ever explicit (N/P, Left/Right, or Enter) and clamps
// at the first/last file instead of wrapping around.
func (s *Server) readFile(term *Terminal, u *user.User, area *file.Area, files []file.File, idx int) error {
	// scrollOffset is how far into the current file's (possibly multi-
	// screen) description the visible window starts -- reset to 0
	// whenever idx changes, since a newly opened file always starts at
	// its own top. See readMessage's identical field for why switching
	// files never happens as a side effect of scrolling past an edge.
	scrollOffset := 0
	for {
		if err := s.Files.MarkFileRead(u.ID, files[idx].ID); err != nil {
			return err
		}
		maxOffset, err := s.drawFileReader(term, area, files, idx, scrollOffset)
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
			if idx < len(files)-1 {
				idx++
				scrollOffset = 0
			}
		case key.Type == KeyChar && (key.Rune == 'd' || key.Rune == 'D'):
			if err := s.downloadFile(term, u, &files[idx]); err != nil {
				return err
			}
		case key.Type == KeyEscape:
			return nil
		case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
			return nil
		}
	}
}

// renderFileReaderHeader returns filread.ans (with AREANAME/FILENUM/
// FILECOUNT filled in), followed by a newline, falling back to a
// plain colored area-name line -- mirrors messages.go's
// renderMessageReaderHeader, including returning a string rather than
// printing directly so drawFileReader can count its line count toward
// the description's scroll viewport budget.
func (s *Server) renderFileReaderHeader(term *Terminal, area *file.Area, idx, total int) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, fileReadScreen))
	if err != nil {
		return ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset + "\n"
	}
	vars := ansi.Vars{
		"BBSNAME":   s.BBSName,
		"AREANAME":  area.Name,
		"FILENUM":   strconv.Itoa(idx + 1),
		"FILECOUNT": strconv.Itoa(total),
	}
	rendered := ansi.Render(raw, vars)
	return finishHeaderLine(ansi.Layout(rendered, term.Width()))
}

// drawFileReader redraws the full reader screen for files[idx]: the
// header banner, the Filename/Size/Uploaded/Downloads metadata block
// (its own customizable screen file), the description -- windowed to
// whatever vertical space is left after the header/meta/footer,
// starting at scrollOffset lines in, so a long description scrolls
// within its own area instead of pushing the header off the top of
// the screen -- and a footer (its own customizable template, scroll
// status + hotkey hint) padded down to the bottom of the screen when
// the description is shorter than the viewport. Mirrors messages.go's
// drawMessageReader in every respect, ANSI-art handling included (see
// ansi.IsPreformatted/ParseGrid there for why a file description
// someone pasted ANSI art into can't just be word-wrapped). Returns
// maxOffset, the largest scrollOffset the caller should still accept
// for this file (0 once the whole description already fits).
func (s *Server) drawFileReader(term *Terminal, area *file.Area, files []file.File, idx, scrollOffset int) (maxOffset int, err error) {
	f := &files[idx]
	body := f.Description

	hint := "[N/Right] Next  [P/Left] Prev  [Up/Dn] Scroll  [D] Download  [Q] Back to list"

	header := s.renderFileReaderHeader(term, area, idx, len(files))

	metaTemplate := s.loadOptionalScreen(fileReadMetaScreen, fallbackFileReadMeta)
	vars := ansi.Vars{
		"FILENAME":  f.Filename,
		"SIZE":      humanize.Bytes(uint64(f.SizeBytes)),
		"DATE":      f.UploadedAt.Format("2006-01-02 15:04"),
		"BY":        f.UploadedByName,
		"DOWNLOADS": strconv.Itoa(f.DownloadCount),
	}
	meta := ansi.Layout(ansi.Render(metaTemplate, vars), term.Width())
	footerTemplate := s.loadOptionalScreen(fileReadFooterScreen, fallbackFileReadFooter)

	var b strings.Builder
	b.WriteString(header)
	b.WriteString(ansi.Reset)
	b.WriteString(meta)
	b.WriteString(ansi.CRLF)

	// Budget the description's viewport the same way drawMessageReader
	// does: header/meta/footer counted from their own rendered text
	// (deployment-customizable, not a fixed line count) plus one
	// always-reserved blank line ahead of the footer, so a long line
	// count doesn't wrap the hotkey hint onto a second physical row.
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
	// Pad with blank lines when the description is shorter than the
	// viewport -- otherwise the footer trails right after a short one
	// instead of staying anchored near the bottom of the screen,
	// mirroring drawFileList/drawMessageReader's identically motivated
	// padding.
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

// downloadFile sends f to the caller via Zmodem (internal/zmodem,
// which shells out to the real "sz" binary -- see its package doc
// comment for why), taking the connection's raw byte stream directly
// for the duration of the transfer (see Terminal.Raw) -- Zmodem is an
// 8-bit binary protocol, nothing like this Terminal's own line-
// oriented/ANSI-cooked interaction, so this bypasses it rather than
// trying to thread binary transfer through it. The caller's own
// terminal client (SyncTERM, NetRunner, ...) auto-detects the
// transfer starting from the "rz\r" invite sz sends first -- no
// separate "press a key to start your receiver" step is needed on a
// modern one.
func (s *Server) downloadFile(term *Terminal, u *user.User, f *file.File) error {
	if _, err := os.Stat(f.StoragePath); err != nil {
		s.logWarn("stat %s for download by %s: %v", f.StoragePath, u.Username, err)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Could not open that file.")
	}

	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		fmt.Sprintf("Starting Zmodem download of %s (%s) -- your terminal should start receiving automatically.", f.Filename, humanize.Bytes(uint64(f.SizeBytes))) +
		ansi.Reset + "\r\n"); err != nil {
		return err
	}

	leftover, sendErr := zmodem.Send(term.Raw(), f.StoragePath)
	if len(leftover) > 0 {
		term.PushBack(leftover)
	}
	switch {
	case sendErr == nil:
		if err := s.Files.RecordDownload(f.ID); err != nil {
			return err
		}
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Download complete.")
	default:
		s.logWarn("zmodem download of %s by %s: %v", f.Filename, u.Username, sendErr)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Download failed or was cancelled.")
	}
}

// uploadFile is the "[U]pload" command from within a file area: it
// receives one or more files from the caller via Zmodem (internal/
// zmodem.Receive, which shells out to the real "rz" binary the same
// way downloadFile's Send shells out to "sz") into a scratch
// directory, then imports whatever actually arrived into area's
// managed storage via internal/file.Store.UploadFile -- the same call
// the web admin's HTTP upload endpoint uses, so a file uploaded
// through either path ends up identical (metadata, on-disk layout,
// duplicate-name handling).
//
// A Zmodem batch can legitimately land more than one file (a caller
// selecting several files at once in their terminal client's own
// upload dialog), and can also legitimately end partway through (the
// caller cancels, or the connection drops) after some files already
// transferred successfully -- so this always imports whatever
// Receive reports actually landed in the scratch directory, even when
// Receive itself also returns an error, rather than discarding
// everything on any failure.
func (s *Server) uploadFile(term *Terminal, u *user.User, area *file.Area) error {
	if !area.CanUpload(u.SecurityLevel) {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "You don't have access to upload here.")
	}

	tmpDir, err := os.MkdirTemp("", "nullmodem-upload-*")
	if err != nil {
		return fmt.Errorf("upload: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		"Ready to receive your file(s) via Zmodem -- start the upload in your terminal now." +
		ansi.Reset + "\r\n"); err != nil {
		return err
	}

	names, leftover, recvErr := zmodem.Receive(term.Raw(), tmpDir)
	if len(leftover) > 0 {
		term.PushBack(leftover)
	}

	var imported, duplicates, failed []string
	for _, name := range names {
		path := filepath.Join(tmpDir, name)
		in, openErr := os.Open(path)
		if openErr != nil {
			s.logWarn("opening received upload %s from %s: %v", name, u.Username, openErr)
			failed = append(failed, name)
			continue
		}
		f, importErr := s.Files.UploadFile(area.ID, u.ID, name, "", in)
		in.Close()
		switch {
		case importErr == nil:
			imported = append(imported, f.Filename)
		case errors.Is(importErr, file.ErrDuplicateFilename):
			duplicates = append(duplicates, name)
		default:
			s.logWarn("importing upload %s from %s into area %d: %v", name, u.Username, area.ID, importErr)
			failed = append(failed, name)
		}
	}
	if len(imported) > 0 {
		s.logInfo("%s uploaded %s to file area %d", u.Username, strings.Join(imported, ", "), area.ID)
	}

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	switch {
	case len(imported) > 0:
		b.WriteString(ansi.FG(ansi.Green, true) + "Received: " + strings.Join(imported, ", ") + ansi.Reset + "\r\n")
	case recvErr == nil:
		b.WriteString(ansi.FG(ansi.Red, true) + "No files were received." + ansi.Reset + "\r\n")
	}
	if len(duplicates) > 0 {
		b.WriteString(ansi.FG(ansi.Yellow, true) + "Skipped (already exists): " + strings.Join(duplicates, ", ") + ansi.Reset + "\r\n")
	}
	if len(failed) > 0 {
		b.WriteString(ansi.FG(ansi.Red, true) + "Failed to store: " + strings.Join(failed, ", ") + ansi.Reset + "\r\n")
	}
	if recvErr != nil {
		if len(imported) == 0 && len(duplicates) == 0 {
			s.logWarn("zmodem upload into file area %d by %s: %v", area.ID, u.Username, recvErr)
			b.WriteString(ansi.FG(ansi.Red, true) + "Upload failed or was cancelled." + ansi.Reset + "\r\n")
		} else {
			s.logWarn("zmodem upload into file area %d by %s ended early: %v", area.ID, u.Username, recvErr)
			b.WriteString(ansi.FG(ansi.Yellow, true) + "Transfer ended early; the files above did make it through." + ansi.Reset + "\r\n")
		}
	}
	return term.Print(b.String())
}

// sysopCreateFileArea is the "builtin:createfilearea" command: it
// prompts for a new area's tag, name, description, and SL gates.
func (s *Server) sysopCreateFileArea(term *Terminal, sysop *user.User) error {
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

	minDownload, err := s.promptSecurityLevel(term, "Minimum SL to download (0-255): ")
	if err != nil {
		return err
	}
	if minDownload < 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid security level.")
	}

	minUpload, err := s.promptSecurityLevel(term, "Minimum SL to upload (0-255): ")
	if err != nil {
		return err
	}
	if minUpload < 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid security level.")
	}

	area, err := s.Files.CreateArea(tag, name, description, network, minDownload, minUpload)
	if err != nil {
		if errors.Is(err, file.ErrTagTaken) {
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "That tag is already in use.")
		}
		return err
	}
	s.logInfo("%s created file area %q (%s)", sysop.Username, area.Name, area.Tag)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Area %q created.", area.Name))
}

// sysopImportFile is the "builtin:importfile" command: an alternative
// to a regular caller's own [U]pload (see uploadFile) for a sysop who
// has already placed a file on the server's filesystem directly (e.g.
// via SCP) and wants to bring it into a chosen area's managed storage
// without transferring it again over Zmodem.
func (s *Server) sysopImportFile(term *Terminal, u *user.User) error {
	areas, err := s.Files.AllAreas()
	if err != nil {
		return err
	}
	if len(areas) == 0 {
		return term.Println(ansi.Reset + "\nNo file areas exist yet. Create one first.")
	}

	if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + "File Areas" + ansi.Reset); err != nil {
		return err
	}
	for i, a := range areas {
		if err := term.Println(fmt.Sprintf("%2d) %s", i+1, a.Name)); err != nil {
			return err
		}
	}
	if err := term.Print(ansi.Reset + "\nImport into which area? " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	choice, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	idx, convErr := strconv.Atoi(strings.TrimSpace(choice))
	if convErr != nil || idx < 1 || idx > len(areas) {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid selection.")
	}
	area := areas[idx-1]

	if err := term.Print(ansi.Reset + "Server-side file path: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	path, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	if err := term.Print(ansi.Reset + "Description: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	description, err := term.ReadLine(false)
	if err != nil {
		return err
	}

	f, err := s.Files.ImportFile(area.ID, u.ID, path, description)
	if err != nil {
		if errors.Is(err, file.ErrDuplicateFilename) {
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "A file with that name already exists in this area.")
		}
		s.logWarn("%s: import into file area %d failed: %v", u.Username, area.ID, err)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + fmt.Sprintf("Import failed: %v", err))
	}
	s.logInfo("%s imported %s (%s) into file area %d", u.Username, f.Filename, humanize.Bytes(uint64(f.SizeBytes)), f.AreaID)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Imported %s (%s).", f.Filename, humanize.Bytes(uint64(f.SizeBytes))))
}
