package bbs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// showFileAreas is the "builtin:files" command: it lists every file
// area the caller can browse and lets them pick one.
func (s *Server) showFileAreas(term *Terminal, u *user.User) error {
	for {
		areas, err := s.Files.ListAreas(u.SecurityLevel)
		if err != nil {
			return err
		}
		if len(areas) == 0 {
			return term.Println(ansi.Reset + "\nNo file areas available.")
		}

		if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + "File Areas" + ansi.Reset); err != nil {
			return err
		}
		for i, a := range areas {
			line := fmt.Sprintf("%2d) %-30s %s", i+1, a.Name, a.Description)
			if err := term.Println(line); err != nil {
				return err
			}
		}
		if err := term.Print(ansi.Reset + "\nSelect an area, or Q to return: " + ansi.FG(ansi.Yellow, true)); err != nil {
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
		if convErr != nil || idx < 1 || idx > len(areas) {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid selection."); err != nil {
				return err
			}
			continue
		}
		if err := s.browseFileArea(term, &areas[idx-1]); err != nil {
			return err
		}
	}
}

// browseFileArea lists an area's files and lets the caller inspect
// one's details, or return to the area list.
func (s *Server) browseFileArea(term *Terminal, area *file.Area) error {
	for {
		files, err := s.Files.ListFiles(area.ID)
		if err != nil {
			return err
		}

		if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset); err != nil {
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
