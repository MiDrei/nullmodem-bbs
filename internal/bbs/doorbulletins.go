package bbs

import (
	"fmt"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/doors"
)

// The doors' bulletins -- the scoreboards and news they write -- from
// the doors menu (B): pick one, page through it.

type doorBulletin struct {
	door  doors.Door
	title string
	file  string
}

func doorBulletinList(available []doors.Door) []doorBulletin {
	var out []doorBulletin
	for _, d := range available {
		for _, b := range d.Bulletins {
			out = append(out, doorBulletin{door: d, title: b.Title, file: b.File})
		}
	}
	return out
}

func (s *Server) showDoorBulletins(term *Terminal, list []doorBulletin) error {
	for {
		var b strings.Builder
		b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + term.T("doors.bulletins") + ansi.Reset + "\r\n")
		for i, x := range list {
			fmt.Fprintf(&b, "%2d) %s\r\n", i+1, toCP437(x.title))
		}
		b.WriteString(" Q) " + term.T("common.back") + "\r\n\r\n" + term.T("doors.which_bulletin") + " " + ansi.FG(ansi.Yellow, true))
		if err := term.Print(b.String()); err != nil {
			return err
		}
		in, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		in = strings.TrimSpace(in)
		if in == "" || strings.EqualFold(in, "q") {
			return term.Print(ansi.Reset)
		}
		n, err := strconv.Atoi(in)
		if err != nil || n < 1 || n > len(list) {
			continue
		}
		if err := s.showDoorBulletin(term, list[n-1]); err != nil {
			return err
		}
	}
}

// showDoorBulletin pages through one bulletin, a screen at a time.
func (s *Server) showDoorBulletin(term *Terminal, x doorBulletin) error {
	dir := x.door.Dir
	if x.door.Kind == "dosbox" {
		dir = x.door.DOSBoxDir
	}
	data, at, err := doors.ReadBulletin(dir, x.file)
	if err != nil {
		if err := term.Println(ansi.Reset + "\r\n" + term.T("doors.bulletin_not_yet", "DOOR", x.door.Name)); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	text := string(data)
	if i := strings.IndexByte(text, 0x1a); i >= 0 {
		text = text[:i] // SAUCE
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	grid := ansi.ParseGrid(text, 80)
	rows := grid.Height
	for rows > 0 && strings.TrimSpace(grid.EncodeRows(rows-1, rows)) == "" {
		rows--
	}
	page := max(5, term.Height()-2)
	for start := 0; start < max(rows, 1); start += page {
		end := min(start+page, rows)
		head := ansi.ClearScreen() + ansi.Reset
		if err := term.Print(head + grid.EncodeRows(start, end) + ansi.Reset); err != nil {
			return err
		}
		more := end < rows
		prompt := fmt.Sprintf("\r\n%s-- %s, %s -- %s", ansi.FG(ansi.White, false), toCP437(x.title), term.Time(at).Format("2006-01-02 15:04"), ansi.Reset)
		if more {
			prompt += term.T("common.more_back") + " "
		} else {
			prompt += term.T("common.enter_back") + " "
		}
		if err := term.Print(prompt); err != nil {
			return err
		}
		in, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(in), "q") || !more {
			return term.Print(ansi.ClearScreen() + ansi.Reset)
		}
	}
	return nil
}
