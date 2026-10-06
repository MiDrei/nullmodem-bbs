package bbs

import (
	"fmt"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

// browseNodelist searches the FTN nodelists: by a system's name, its
// sysop, its place or an address prefix ("21:1/").
func (s *Server) browseNodelist(term *Terminal, u *user.User) error {
	if s.Nodelist == nil {
		return nil
	}
	imps, err := s.Nodelist.Imports()
	if err != nil {
		return err
	}
	header := s.featureHeader(term, u, "nodelist.ans", term.T("common.nodelists"))
	var b strings.Builder
	b.WriteString(header)
	if len(imps) == 0 {
		b.WriteString("  " + fgDim(ansi.White) + term.T("nodelist.none") + ansi.Reset + "\r\n")
		if err := term.Print(b.String()); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	for _, i := range imps {
		fmt.Fprintf(&b, "  %s%-12s%s%s  %s%s%s\r\n", ansi.FG(ansi.Cyan, true), i.Network, fgDim(ansi.White),
			term.T("nodelist.systems", "COUNT", fmt.Sprintf("%4d", i.Entries)), ansi.FG(ansi.Black, true), i.Filename, ansi.Reset)
	}
	if err := term.Print(b.String()); err != nil {
		return err
	}
	cut := func(v string, n int) string {
		r := []rune(v)
		if len(r) > n {
			r = r[:n]
		}
		return string(r) + strings.Repeat(" ", n-len(r))
	}
	for {
		if err := term.Print(ansi.Reset + "\r\n  " + fgDim(ansi.White) + term.T("nodelist.search_prompt") + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		q, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		if q = strings.TrimSpace(fromCP437(q)); q == "" {
			return term.Print(ansi.Reset)
		}
		const limit = 200
		found, err := s.Nodelist.Search("", q, limit)
		if err != nil {
			return err
		}
		// A page of its own, as many systems as fit above the prompt.
		count := strconv.Itoa(len(found))
		if len(found) == limit {
			count += "+"
		}
		fits := max(3, term.Height()-strings.Count(header, "\n")-8)
		b.Reset()
		b.WriteString(header)
		b.WriteString("  " + fgDim(ansi.White) + term.T("nodelist.result", "QUERY", ansi.FG(ansi.White, true)+toCP437(q)+fgDim(ansi.White), "COUNT", count) + ansi.Reset + "\r\n\r\n")
		if len(found) == 0 {
			b.WriteString("  " + term.T("common.nothing_found") + "\r\n")
		} else {
			b.WriteString("  " + fgDim(ansi.White) + padCP(term.T("common.address"), 13) + padCP(term.T("nodelist.col_system"), 25) +
				padCP(term.T("common.sysop"), 19) + term.T("common.location") + "\r\n" + fgDim(ansi.Blue) + "  " + strings.Repeat("\xc4", 76) + ansi.Reset + "\r\n")
		}
		for i, e := range found {
			if i == fits {
				b.WriteString("  " + ansi.FG(ansi.Black, true) + term.T("common.first_n", "COUNT", fits) + ansi.Reset + "\r\n")
				break
			}
			state := ""
			if e.Keyword != "" && !strings.EqualFold(e.Keyword, "pvt") {
				state = " " + e.Keyword
			}
			fmt.Fprintf(&b, "  %s%s %s%s %s%s %s%s%s%s\r\n",
				ansi.FG(ansi.Cyan, true), cut(e.Address(), 12),
				ansi.FG(ansi.White, true), toCP437(cut(e.Name, 24)),
				fgDim(ansi.White), toCP437(cut(e.Sysop, 18)),
				ansi.FG(ansi.Black, true), toCP437(cut(e.Location, 14)), ansi.FG(ansi.Red, true)+state, ansi.Reset)
		}
		if err := term.Print(b.String()); err != nil {
			return err
		}
	}
}
