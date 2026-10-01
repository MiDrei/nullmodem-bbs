package bbs

import (
	"fmt"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

// browseNodelist searches the FTN nodelists: by a system's name, its
// sysop, its place or an address prefix ("21:1/").
func (s *Server) browseNodelist(term *Terminal, _ *user.User) error {
	if s.Nodelist == nil {
		return nil
	}
	imps, err := s.Nodelist.Imports()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + "  Nodelists" + ansi.Reset + "\r\n")
	if len(imps) == 0 {
		b.WriteString("  None yet -- they arrive with the networks' file echoes.\r\n")
		if err := term.Print(b.String()); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	for _, i := range imps {
		fmt.Fprintf(&b, "  %s%-12s%s %4d systems  (%s)\r\n", ansi.FG(ansi.Yellow, true), i.Network, ansi.Reset, i.Entries, i.Filename)
	}
	if err := term.Print(b.String()); err != nil {
		return err
	}
	for {
		if err := term.Print(ansi.Reset + "\r\nSearch (name, sysop, place or address like 21:1/; Enter = back): " + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		q, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		if q = strings.TrimSpace(fromCP437(q)); q == "" {
			return term.Print(ansi.Reset)
		}
		found, err := s.Nodelist.Search("", q, 40)
		if err != nil {
			return err
		}
		b.Reset()
		b.WriteString(ansi.Reset)
		if len(found) == 0 {
			b.WriteString("  Nothing found.\r\n")
		}
		cut := func(v string, n int) string {
			r := []rune(v)
			if len(r) > n {
				r = r[:n]
			}
			return string(r) + strings.Repeat(" ", n-len(r))
		}
		for _, e := range found {
			state := ""
			if e.Keyword != "" && !strings.EqualFold(e.Keyword, "pvt") {
				state = " " + e.Keyword
			}
			fmt.Fprintf(&b, "  %s%s%s%s %s%s%s%s\r\n",
				ansi.FG(ansi.Yellow, true), cut(e.Address(), 12),
				ansi.FG(ansi.White, true), toCP437(cut(e.Name, 24)),
				ansi.FG(ansi.White, false), toCP437(cut(e.Sysop, 18)),
				ansi.FG(ansi.Cyan, false), toCP437(cut(e.Location, 14))+ansi.FG(ansi.Red, false)+state+ansi.Reset)
		}
		if len(found) == 40 {
			b.WriteString("  (the first 40 -- search more precisely)\r\n")
		}
		if err := term.Print(b.String()); err != nil {
			return err
		}
	}
}
