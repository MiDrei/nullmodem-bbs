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
				if err := s.Files.MarkAreaRead(u.ID, area.ID); err != nil {
					return err
				}
				if err := s.browseFileArea(term, &area); err != nil {
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
)

const (
	fallbackFileAreaRow         = "{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}"
	fallbackFileAreaRowSelected = "\x1b[47m\x1b[30m{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}\x1b[0m"
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

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(s.loadOptionalScreen(fileAreaColumnsScreen, fallbackFileAreaColumns))
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

// browseFileArea lists an area's files and lets the caller inspect
// one's details, or return to the area list.
func (s *Server) browseFileArea(term *Terminal, area *file.Area) error {
	for {
		files, err := s.Files.ListFiles(area.ID)
		if err != nil {
			return err
		}

		if err := s.printFileListHeader(term, area); err != nil {
			return err
		}
		if len(files) == 0 {
			if err := term.Println("(no files yet)"); err != nil {
				return err
			}
		}
		for i, f := range files {
			line := fmt.Sprintf("%3d) %-30s %10s  %s", i+1, f.Filename, humanize.Bytes(uint64(f.SizeBytes)), f.Description)
			if err := term.Println(line); err != nil {
				return err
			}
		}
		if err := term.Print(ansi.Reset + "\nFile # for details, or Q to return: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}

		choice, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		choice = strings.TrimSpace(choice)
		if choice == "" || strings.EqualFold(choice, "Q") {
			return nil
		}

		idx, convErr := strconv.Atoi(choice)
		if convErr != nil || idx < 1 || idx > len(files) {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid selection."); err != nil {
				return err
			}
			continue
		}
		if err := s.showFileDetails(term, &files[idx-1]); err != nil {
			return err
		}
	}
}

func (s *Server) showFileDetails(term *Terminal, f *file.File) error {
	rule := ansi.FG(ansi.Cyan, true) + strings.Repeat("-", 40) + ansi.Reset
	lines := []string{
		"",
		rule,
		fmt.Sprintf("Filename:  %s", f.Filename),
		fmt.Sprintf("Size:      %s", humanize.Bytes(uint64(f.SizeBytes))),
		fmt.Sprintf("Uploaded:  %s by %s", f.UploadedAt.Format("2006-01-02 15:04"), f.UploadedByName),
		fmt.Sprintf("Downloads: %d", f.DownloadCount),
		rule,
		f.Description,
		rule,
	}
	for _, line := range lines {
		if err := term.Println(line); err != nil {
			return err
		}
	}
	return nil
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

	area, err := s.Files.CreateArea(tag, name, description, minDownload, minUpload)
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
