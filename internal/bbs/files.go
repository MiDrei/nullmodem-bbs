package bbs

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// showFileAreas is the "builtin:files" command: a lightbar over every
// file area the caller can browse, showing each area's Total/New/
// Yours file counts and letting them move the highlighted row with
// the arrow keys, Enter to browse that area, Q/Escape to return --
// the same interaction internal/bbs/messages.go's showAreas uses for
// message areas.
func (s *Server) showFileAreas(term *Terminal, u *user.User) error {
	selected := 0
outer:
	for {
		stats, err := s.Files.ListAreaStats(u.SecurityLevel, u.ID)
		if err != nil {
			return err
		}
		if len(stats) == 0 {
			if err := s.printAreaHeader(term, u, "filareas.ans", "File Areas"); err != nil {
				return err
			}
			return term.Println(ansi.Reset + "\nNo file areas available.")
		}
		if selected >= len(stats) {
			selected = len(stats) - 1
		}

		for {
			if err := s.drawFileAreaLightbar(term, u, stats, selected); err != nil {
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

// drawFileAreaLightbar mirrors messages.go's drawAreaLightbar exactly,
// against the file-area column/row screen files and file.AreaWithStats
// instead of message.AreaWithStats.
func (s *Server) drawFileAreaLightbar(term *Terminal, u *user.User, stats []file.AreaWithStats, selected int) error {
	if err := s.printAreaHeader(term, u, "filareas.ans", "File Areas"); err != nil {
		return err
	}

	rowTemplate := s.loadOptionalScreen(fileAreaRowScreen, fallbackFileAreaRow)
	rowSelectedTemplate := s.loadOptionalScreen(fileAreaRowSelectedScreen, fallbackFileAreaRowSelected)
	networkTemplate := s.loadOptionalScreen(fileAreaNetworkScreen, fallbackFileAreaNetwork)

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(s.loadOptionalScreen(fileAreaColumnsScreen, fallbackFileAreaColumns))
	b.WriteString(ansi.CRLF)

	// stats is sorted network, sort_order, name (see ListAreaStats), so
	// every area sharing a network is already contiguous -- see
	// messages.go's drawAreaLightbar, which this mirrors.
	lastNetwork := ""
	for i, st := range stats {
		if st.Area.Network != lastNetwork {
			if st.Area.Network != "" {
				b.WriteString(ansi.Layout(ansi.Render(networkTemplate, ansi.Vars{"NETWORK": st.Area.Network}), term.Width()))
				b.WriteString(ansi.CRLF)
			}
			lastNetwork = st.Area.Network
		}

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

// fileListScreen is the hand-designed banner shown above an area's
// file list -- see messages.go's msgListScreen doc comment for why
// this clear-screen-then-banner is needed instead of printing the
// list inline over whatever screen (e.g. the area lightbar) was there
// before.
const fileListScreen = "fillist.ans"

// printFileListHeader shows fillist.ans (with AREANAME filled in),
// falling back to a plain colored area-name line on a cleared screen.
func (s *Server) printFileListHeader(term *Terminal, area *file.Area) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, fileListScreen))
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

// browseFileArea is a lightbar over an area's files -- the same
// interaction as messages.go's browseArea over messages: arrow keys
// move the highlight, Enter opens the file reader at that file, Q/
// Escape returns to the area list. Uploading isn't part of this loop
// (see sysopImportFile's doc comment), so there's no equivalent to
// browseArea's P handling here.
func (s *Server) browseFileArea(term *Terminal, u *user.User, area *file.Area) error {
	selected := 0
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
			if key.Type == KeyEscape || (key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q')) {
				return nil
			}
			continue
		}
		if selected >= len(files) {
			selected = len(files) - 1
		}

		for {
			if err := s.drawFileList(term, area, files, selected, readIDs); err != nil {
				return err
			}
			key, err := term.ReadKey()
			if err != nil {
				return err
			}
			switch {
			case key.Type == KeyUp:
				selected = (selected - 1 + len(files)) % len(files)
			case key.Type == KeyDown:
				selected = (selected + 1) % len(files)
			case key.Type == KeyEnter:
				if err := s.readFile(term, u, area, files, selected); err != nil {
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
	if err := s.printFileListHeader(term, area); err != nil {
		return err
	}
	return term.Print(ansi.Reset + "\n(no files yet)\r\n\r\n" + ansi.FG(ansi.White, true) + "[Q] Back" + ansi.Reset)
}

// drawFileList redraws the header banner plus the Filename/By/Size/
// Date table, with the row at selected highlighted -- the file list's
// equivalent of messages.go's drawMessageList.
func (s *Server) drawFileList(term *Terminal, area *file.Area, files []file.File, selected int, readIDs map[int64]bool) error {
	if err := s.printFileListHeader(term, area); err != nil {
		return err
	}

	rowTemplate := s.loadOptionalScreen(fileListRowScreen, fallbackFileListRow)
	rowSelectedTemplate := s.loadOptionalScreen(fileListRowSelectedScreen, fallbackFileListRowSelected)

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(s.loadOptionalScreen(fileListColumnsScreen, fallbackFileListColumns))
	b.WriteString(ansi.CRLF)

	for i, f := range files {
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
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + "[Up/Down] Move   [Enter] View   [Q] Back" + ansi.Reset)
	return term.Print(b.String())
}

// Fixed filenames for the hand-designed pieces of the file reader,
// mirroring messages.go's msgread.ans/msgread-meta.ans.
const (
	fileReadScreen     = "filread.ans"
	fileReadMetaScreen = "filread-meta.ans"
)

var fallbackFileReadMeta = "\x1b[1;32mFilename:  \x1b[1;37m{FILENAME:-40}\x1b[1;32m Size: \x1b[1;37m{SIZE}\r\n" +
	"\x1b[1;32mUploaded:  \x1b[1;37m{DATE}\x1b[1;32m by \x1b[1;37m{BY}\r\n" +
	"\x1b[1;32mDownloads: \x1b[1;37m{DOWNLOADS}\r\n" +
	"\x1b[32m" + strings.Repeat("-", 79) + ansi.Reset

// readFile is a file-details reader over files, starting at idx, that
// lets the caller page through every file in the area with the arrow
// keys / N,P without returning to the list each time -- mirroring
// messages.go's readMessage, including clamping at the first/last
// file instead of wrapping around.
func (s *Server) readFile(term *Terminal, u *user.User, area *file.Area, files []file.File, idx int) error {
	for {
		if err := s.Files.MarkFileRead(u.ID, files[idx].ID); err != nil {
			return err
		}
		if err := s.drawFileReader(term, area, files, idx); err != nil {
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
			if idx < len(files)-1 {
				idx++
			}
		case key.Type == KeyEscape:
			return nil
		case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
			return nil
		}
	}
}

// printFileReaderHeader shows filread.ans (with AREANAME/FILENUM/
// FILECOUNT filled in), falling back to a plain colored area-name
// line -- mirroring messages.go's printMessageReaderHeader.
func (s *Server) printFileReaderHeader(term *Terminal, area *file.Area, idx, total int) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, fileReadScreen))
	if err != nil {
		return term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset)
	}
	vars := ansi.Vars{
		"BBSNAME":   s.BBSName,
		"AREANAME":  area.Name,
		"FILENUM":   strconv.Itoa(idx + 1),
		"FILECOUNT": strconv.Itoa(total),
	}
	rendered := ansi.Render(raw, vars)
	return term.Println(ansi.Layout(rendered, term.Width()))
}

// drawFileReader redraws the full reader screen for files[idx]: the
// header banner, the Filename/Size/Uploaded/Downloads metadata block
// (its own customizable screen file), the word-wrapped description,
// and a footer hinting at the navigation keys -- mirroring
// messages.go's drawMessageReader.
func (s *Server) drawFileReader(term *Terminal, area *file.Area, files []file.File, idx int) error {
	if err := s.printFileReaderHeader(term, area, idx, len(files)); err != nil {
		return err
	}
	f := &files[idx]

	metaTemplate := s.loadOptionalScreen(fileReadMetaScreen, fallbackFileReadMeta)
	vars := ansi.Vars{
		"FILENAME":  f.Filename,
		"SIZE":      humanize.Bytes(uint64(f.SizeBytes)),
		"DATE":      f.UploadedAt.Format("2006-01-02 15:04"),
		"BY":        f.UploadedByName,
		"DOWNLOADS": strconv.Itoa(f.DownloadCount),
	}

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(ansi.Layout(ansi.Render(metaTemplate, vars), term.Width()))
	b.WriteString(ansi.CRLF)
	for _, line := range ansi.WrapText(f.Description, term.Width()) {
		b.WriteString(ansi.Reset + line + ansi.CRLF)
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + "[Enter/Dn/Right] Next  [Up/Left] Prev  [Q] Back to list" + ansi.Reset)
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

// sysopImportFile is the "builtin:importfile" command. There is no
// in-session upload protocol (see internal/file's package doc), so it
// imports a file the sysop has already placed on the server's
// filesystem (e.g. via SCP) into a chosen area's managed storage.
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
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + fmt.Sprintf("Import failed: %v", err))
	}
	s.logInfo("%s imported %s (%s) into file area %d", u.Username, f.Filename, humanize.Bytes(uint64(f.SizeBytes)), f.AreaID)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Imported %s (%s).", f.Filename, humanize.Bytes(uint64(f.SizeBytes))))
}
