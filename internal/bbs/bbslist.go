package bbs

import (
	"fmt"
	"strings"

	"github.com/midrei/nullmodem-kit/ansi"

	"github.com/midrei/nullmodem-bbs/internal/community"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// The BBS list: other boards, kept by the callers -- anyone approved
// may add one and change or remove their own; the sysop any.

func (s *Server) bbsList(term *Terminal, u *user.User) error {
	if s.Community == nil {
		return nil
	}
	cut := func(v string, n int) string {
		r := []rune(v)
		if len(r) > n {
			r = r[:n]
		}
		return string(r) + strings.Repeat(" ", n-len(r))
	}
	statusWidth := 0
	for _, k := range []string{"bbslist.st_up", "bbslist.st_down", "bbslist.st_unknown"} {
		statusWidth = max(statusWidth, len(term.T(k)))
	}
	keys := term.T("bbslist.keys_view")
	if u.Validated {
		keys = term.T("bbslist.keys")
	}
	// A lightbar like the area lists': as many boards as fit under the
	// banner, the column titles and the key line, scrolling through
	// the rest.
	cur, top := 0, 0
	for {
		list, err := s.Community.BBSList()
		if err != nil {
			return err
		}
		header := s.featureHeader(term, u, "bbslist.ans", term.T("common.bbs_list"))
		rows := max(3, term.Height()-strings.Count(header, "\n")-4)
		cur = max(0, min(cur, len(list)-1))
		if cur < top {
			top = cur
		}
		if cur >= top+rows {
			top = cur - rows + 1
		}
		var b strings.Builder
		b.WriteString(header)
		if len(list) == 0 {
			b.WriteString("  " + fgDim(ansi.White) + term.T("bbslist.empty") + ansi.Reset + "\r\n")
		} else {
			b.WriteString("  " + fgDim(ansi.White) + padCP(term.T("common.bbs"), 31) + padCP(term.T("common.address"), 33) + term.T("bbslist.col_status") + "\r\n" +
				fgDim(ansi.Blue) + "  " + strings.Repeat("\xc4", 76) + ansi.Reset + "\r\n")
		}
		end := min(top+rows, len(list))
		for i := top; i < end; i++ {
			e := list[i]
			if i == cur {
				b.WriteString("\x1b[1;37;44m  " + toCP437(cut(e.Name, 30)) + " " + toCP437(cut(e.Address, 32)) + " " + padCP(bbsStatusText(term, e), statusWidth+1) + ansi.Reset + "\r\n")
				continue
			}
			fmt.Fprintf(&b, "  %s%s %s%s %s%s\r\n", ansi.FG(ansi.White, true), toCP437(cut(e.Name, 30)),
				fgDim(ansi.White), toCP437(cut(e.Address, 32)), bbsStatus(term, e, statusWidth), ansi.Reset)
		}
		for i := end - top; i < rows && len(list) > 0; i++ {
			b.WriteString("\r\n")
		}
		scroll := ""
		if len(list) > rows {
			scroll = term.T("list.range", "FROM", top+1, "TO", end, "TOTAL", len(list))
		}
		b.WriteString(scrollRule(term, scroll) + "\r\n" + keyHints(keys) + ansi.Reset)
		if err := term.Print(b.String()); err != nil {
			return err
		}
		k, err := term.ReadKey()
		if err != nil {
			return err
		}
		switch {
		case k.Type == KeyUp:
			cur--
		case k.Type == KeyDown:
			cur++
		case k.Type == KeyPgUp:
			cur -= rows
		case k.Type == KeyPgDn:
			cur += rows
		case k.Type == KeyHome:
			cur = 0
		case k.Type == KeyEnd:
			cur = len(list) - 1
		case k.Type == KeyEnter && len(list) > 0:
			if err := s.showBBS(term, u, list[cur]); err != nil {
				return err
			}
		case isKey(k, 'a') && u.Validated:
			term.Print(ansi.Reset + "\r\n\r\n")
			if err := s.editBBS(term, u, community.BBS{}); err != nil {
				return err
			}
		case isKey(k, 'q'), k.Type == KeyEscape:
			return term.Print(ansi.Reset)
		}
	}
}

func (s *Server) mayChangeBBS(u *user.User, e community.BBS) bool {
	return u.Validated && (e.AddedByID == u.ID || u.SecurityLevel >= user.SLSysop)
}

func (s *Server) showBBS(term *Terminal, u *user.User, e community.BBS) error {
	var b strings.Builder
	// The labels as wide as the widest; a long value wraps under itself.
	labels := []string{"common.address", "common.sysop", "common.software", "bbslist.about", "bbslist.added_by", "bbslist.online"}
	labelWidth := 0
	for _, k := range labels {
		labelWidth = max(labelWidth, len(term.T(k)))
	}
	indent := 2 + labelWidth + 1
	// row prints v, CP437 already (user data goes through toCP437 first).
	row := func(key, v string) {
		if v == "" {
			return
		}
		lines := ansi.WrapText(v, max(20, term.Width()-indent-1))
		for i, l := range lines {
			label := ""
			if i == 0 {
				label = term.T(key)
			}
			fmt.Fprintf(&b, "  %s%s%s %s\r\n", fgDim(ansi.Cyan), padCP(label, labelWidth), ansi.Reset, l)
		}
	}
	b.WriteString(s.featureHeader(term, u, "bbslist.ans", term.T("common.bbs_list")))
	b.WriteString("  " + ansi.FG(ansi.White, true) + toCP437(e.Name) + ansi.Reset + "\r\n")
	row("common.address", toCP437(e.Address))
	row("common.sysop", toCP437(e.Sysop))
	row("common.software", toCP437(e.Software))
	row("bbslist.about", toCP437(e.Description))
	row("bbslist.added_by", toCP437(e.AddedBy))
	switch {
	case e.CheckedAt.IsZero():
		row("bbslist.online", term.T("common.not_checked_yet"))
	case e.Online:
		row("bbslist.online", term.T("bbslist.up", "WHEN", term.Time(e.CheckedAt).Format("2006-01-02 15:04")))
	case !e.LastUpAt.IsZero():
		row("bbslist.online", term.T("bbslist.down_seen", "WHEN", term.Time(e.LastUpAt).Format("2006-01-02")))
	default:
		row("bbslist.online", term.T("bbslist.down"))
	}
	if !s.mayChangeBBS(u, e) {
		if err := term.Print(b.String()); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	b.WriteString("\r\n  " + keyHints(term.T("bbslist.entry_keys")) + " " + ansi.FG(ansi.Yellow, true))
	if err := term.Print(b.String()); err != nil {
		return err
	}
	in, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	switch strings.ToUpper(strings.TrimSpace(in)) {
	case "E":
		return s.editBBS(term, u, e)
	case "D":
		if err := s.Community.DeleteBBS(e.ID); err != nil {
			return err
		}
		s.logInfo("%s removed %s from the BBS list", u.Username, e.Name)
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "  " + term.T("bbslist.removed") + ansi.Reset)
	}
	return term.Print(ansi.Reset)
}

// editBBS asks for each field (Enter keeps what's there) and saves.
func (s *Server) editBBS(term *Terminal, u *user.User, e community.BBS) error {
	ask := func(label, cur string, max int) (string, error) {
		hint := ""
		if cur != "" {
			hint = " [" + toCP437(cur) + "]"
		}
		if err := term.Print(ansi.Reset + "  " + term.T("bbslist.ask", "LABEL", label, "MAX", max) + hint + ": " + ansi.FG(ansi.Yellow, true)); err != nil {
			return "", err
		}
		v, err := term.ReadLine(false)
		if err != nil {
			return "", err
		}
		if v = strings.TrimSpace(fromCP437(v)); v == "" {
			return cur, nil
		}
		return v, nil
	}
	var err error
	if err = term.Print(ansi.Reset + "\r\n"); err != nil {
		return err
	}
	if e.Name, err = ask(term.T("common.name"), e.Name, community.MaxBBSName); err != nil {
		return err
	}
	if e.Address, err = ask(term.T("common.address_host_port"), e.Address, community.MaxBBSField); err != nil {
		return err
	}
	if e.Sysop, err = ask(term.T("common.sysop"), e.Sysop, community.MaxBBSField); err != nil {
		return err
	}
	if e.Software, err = ask(term.T("common.software"), e.Software, community.MaxBBSField); err != nil {
		return err
	}
	if e.Description, err = ask(term.T("common.about_it"), e.Description, community.MaxBBSDesc); err != nil {
		return err
	}
	if e.ID == 0 {
		e.AddedByID, e.AddedBy = u.ID, u.Username
	}
	if e.Name == "" || e.Address == "" {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "  " + term.T("bbslist.incomplete") + ansi.Reset)
	}
	if _, err := s.Community.SaveBBS(e); err != nil {
		return err
	}
	s.logInfo("%s saved %s in the BBS list", u.Username, e.Name)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "  " + term.T("common.saved") + ansi.Reset)
}

// bbsStatus is the online check's verdict, width columns wide.
// bbsStatusText is bbsStatus's word, without its colour.
func bbsStatusText(term *Terminal, e community.BBS) string {
	switch {
	case e.CheckedAt.IsZero():
		return term.T("bbslist.st_unknown")
	case e.Online:
		return term.T("bbslist.st_up")
	}
	return term.T("bbslist.st_down")
}

func bbsStatus(term *Terminal, e community.BBS, width int) string {
	switch {
	case e.CheckedAt.IsZero():
		return fgDim(ansi.White) + padCP(term.T("bbslist.st_unknown"), width)
	case e.Online:
		return ansi.FG(ansi.Green, true) + padCP(term.T("bbslist.st_up"), width)
	}
	return ansi.FG(ansi.Red, true) + padCP(term.T("bbslist.st_down"), width)
}
