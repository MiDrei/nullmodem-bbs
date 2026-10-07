package bbs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/midrei/nullmodem-kit/ansi"
	"github.com/dustin/go-humanize"

	"github.com/midrei/nullmodem-bbs/internal/file"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// New files and file search: across all the areas a caller may
// download from, what arrived since they last looked (not opened yet --
// the same "new" as in the file lists) and a search by name or
// description. A number opens a file (details, download).

const newFilesMax = 200

// newFiles lists the caller's unopened files, newest first.
func (s *Server) newFiles(term *Terminal, u *user.User) error {
	files, total, err := s.Files.UnreadFiles(u.ID, u.SecurityLevel, newFilesMax)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return s.scanNote(term, term.T("files.no_new"))
	}
	title := term.T("files.new_title")
	if total > len(files) {
		title = term.T("files.new_title_cut", "COUNT", len(files), "TOTAL", total)
	}
	return s.fileList(term, u, title, files, true)
}

// searchFiles asks for words and lists what matches.
func (s *Server) searchFiles(term *Terminal, u *user.User) error {
	if err := term.Print(ansi.Reset + "\r\n" + keyHints(term.T("files.search_prompt")) + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	q, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	if q = strings.TrimSpace(q); q == "" {
		return term.Print(ansi.Reset)
	}
	files, err := s.Files.SearchFiles(u.SecurityLevel, q, 100)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return s.scanNote(term, term.T("common.nothing_found"))
	}
	return s.fileList(term, u, term.T("files.matching", "QUERY", q), files, false)
}

// newFilesScreen is the banner over the new files and file search
// lists; its {TITLE} says which.
const newFilesScreen = "newfiles.ans"

// fileList pages through files, two lines each; a number opens one,
// A (offerAllSeen) marks everything seen.
func (s *Server) fileList(term *Terminal, u *user.User, title string, files []file.File, offerAllSeen bool) error {
	areas := map[int64]*file.Area{}
	areaOf := func(id int64) *file.Area {
		if a, ok := areas[id]; ok {
			return a
		}
		a, err := s.Files.AreaByID(id)
		if err != nil {
			a = &file.Area{ID: id, Tag: "?", Name: "?"}
		}
		areas[id] = a
		return a
	}
	header := s.featureHeader(term, u, newFilesScreen, title)
	perPage := max(3, (term.Height()-strings.Count(header, "\n")-2)/2)
	start := 0
	for {
		end := min(start+perPage, len(files))
		var b strings.Builder
		b.WriteString(header)
		for i := start; i < end; i++ {
			f := files[i]
			name := []rune(f.Filename)
			if len(name) > 26 {
				name = name[:26]
			}
			tag := []rune(areaOf(f.AreaID).Tag)
			if len(tag) > 16 {
				tag = tag[:16]
			}
			fmt.Fprintf(&b, "  %s%4d%s  %s%-26s%s %8s  %s  %s%s%s\r\n",
				ansi.FG(ansi.Yellow, true), i+1, ansi.Reset,
				ansi.FG(ansi.White, true), string(name), ansi.Reset,
				humanize.Bytes(uint64(f.SizeBytes)), term.Time(f.UploadedAt).Format("2006-01-02"),
				ansi.FG(ansi.Cyan, false), string(tag), ansi.Reset)
			desc := strings.TrimSpace(strings.SplitN(strings.ReplaceAll(f.Description, "\r", ""), "\n", 2)[0])
			if r := []rune(desc); len(r) > 70 {
				desc = string(r[:70])
			}
			fmt.Fprintf(&b, "        %s%s%s\r\n", ansi.FG(ansi.White, false), desc, ansi.Reset)
		}
		keys := term.T("list.key_open")
		if end < len(files) {
			keys += "  " + term.T("list.key_more")
		}
		if offerAllSeen {
			keys += "  " + term.T("files.key_all_seen")
		}
		keys += "  " + term.T("list.key_back")
		fmt.Fprintf(&b, "%s\r\n%s%s (%s): %s", scrollRule(term, ""), keyHints(keys), ansi.Reset, term.T("list.range", "FROM", start+1, "TO", end, "TOTAL", len(files)), ansi.FG(ansi.Yellow, true))
		if err := term.Print(b.String()); err != nil {
			return err
		}
		in, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		in = strings.ToUpper(strings.TrimSpace(in))
		switch {
		case in == "Q":
			return term.Print(ansi.Reset)
		case in == "" && end < len(files):
			start = end
		case in == "":
			return term.Print(ansi.Reset)
		case in == "A" && offerAllSeen:
			n, err := s.Files.MarkAllFilesRead(u.ID, u.SecurityLevel)
			if err != nil {
				return err
			}
			return s.scanNote(term, term.N("files.marked_seen", int(n)))
		default:
			if n, err := strconv.Atoi(in); err == nil && n >= 1 && n <= len(files) {
				f := files[n-1]
				if err := s.readFile(term, u, areaOf(f.AreaID), []file.File{f}, 0); err != nil {
					return err
				}
			}
		}
	}
}
