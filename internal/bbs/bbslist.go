package bbs

import (
	"fmt"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/community"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// The BBS list: other boards, kept by the callers -- anyone approved
// may add one and change or remove their own; the sysop any.

func (s *Server) bbsList(term *Terminal, u *user.User) error {
	if s.Community == nil {
		return nil
	}
	for {
		list, err := s.Community.BBSList()
		if err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + "  " + term.T("bbslist.title") + ansi.Reset + "\r\n")
		if len(list) == 0 {
			b.WriteString("  " + term.T("bbslist.empty") + "\r\n")
		}
		cut := func(v string, n int) string {
			r := []rune(v)
			if len(r) > n {
				r = r[:n]
			}
			return string(r) + strings.Repeat(" ", n-len(r))
		}
		for i, e := range list {
			fmt.Fprintf(&b, "  %s%3d%s  %s%s %s%s %s%s\r\n", ansi.FG(ansi.Yellow, true), i+1, ansi.Reset,
				ansi.FG(ansi.White, true), toCP437(cut(e.Name, 30)), ansi.FG(ansi.Cyan, false), toCP437(cut(e.Address, 32)),
				bbsStatus(e), ansi.Reset)
		}
		keys := term.T("bbslist.key_details")
		if u.Validated {
			keys += ", " + term.T("bbslist.key_add")
		}
		b.WriteString("\r\n  " + keys + " (" + term.T("common.enter_back") + "): " + ansi.FG(ansi.Yellow, true))
		if err := term.Print(b.String()); err != nil {
			return err
		}
		in, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		in = strings.ToUpper(strings.TrimSpace(in))
		switch {
		case in == "":
			return term.Print(ansi.Reset)
		case in == "A" && u.Validated:
			if err := s.editBBS(term, u, community.BBS{}); err != nil {
				return err
			}
		default:
			if n, err := strconv.Atoi(in); err == nil && n >= 1 && n <= len(list) {
				if err := s.showBBS(term, u, list[n-1]); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) mayChangeBBS(u *user.User, e community.BBS) bool {
	return u.Validated && (e.AddedByID == u.ID || u.SecurityLevel >= user.SLSysop)
}

func (s *Server) showBBS(term *Terminal, u *user.User, e community.BBS) error {
	var b strings.Builder
	row := func(label, v string) {
		if v != "" {
			fmt.Fprintf(&b, "  %s%-12s%s %s\r\n", ansi.FG(ansi.Cyan, false), label, ansi.Reset, toCP437(v))
		}
	}
	b.WriteString(ansi.Reset + "\r\n  " + ansi.FG(ansi.White, true) + toCP437(e.Name) + ansi.Reset + "\r\n")
	row(term.T("bbslist.address"), e.Address)
	row(term.T("bbslist.sysop"), e.Sysop)
	row(term.T("bbslist.software"), e.Software)
	row(term.T("bbslist.about"), e.Description)
	row(term.T("bbslist.added_by"), e.AddedBy)
	switch {
	case e.CheckedAt.IsZero():
		row(term.T("bbslist.online"), term.T("bbslist.not_checked"))
	case e.Online:
		row(term.T("bbslist.online"), term.T("bbslist.up", "WHEN", term.Time(e.CheckedAt).Format("2006-01-02 15:04")))
	case !e.LastUpAt.IsZero():
		row(term.T("bbslist.online"), term.T("bbslist.down_seen", "WHEN", term.Time(e.LastUpAt).Format("2006-01-02")))
	default:
		row(term.T("bbslist.online"), term.T("bbslist.down"))
	}
	if !s.mayChangeBBS(u, e) {
		if err := term.Print(b.String()); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
	b.WriteString("\r\n  " + term.T("bbslist.entry_keys") + " " + ansi.FG(ansi.Yellow, true))
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
	if e.Name, err = ask(term.T("bbslist.name"), e.Name, community.MaxBBSName); err != nil {
		return err
	}
	if e.Address, err = ask(term.T("bbslist.address_hint"), e.Address, community.MaxBBSField); err != nil {
		return err
	}
	if e.Sysop, err = ask(term.T("bbslist.sysop"), e.Sysop, community.MaxBBSField); err != nil {
		return err
	}
	if e.Software, err = ask(term.T("bbslist.software"), e.Software, community.MaxBBSField); err != nil {
		return err
	}
	if e.Description, err = ask(term.T("bbslist.about_it"), e.Description, community.MaxBBSDesc); err != nil {
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

// bbsStatus is the online check's verdict, five columns wide.
func bbsStatus(e community.BBS) string {
	switch {
	case e.CheckedAt.IsZero():
		return ansi.FG(ansi.White, false) + "  ?  "
	case e.Online:
		return ansi.FG(ansi.Green, true) + " up  "
	}
	return ansi.FG(ansi.Red, true) + "down "
}
