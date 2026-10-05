package bbs

import (
	"fmt"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/lastcallers"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
)

// lastCallersShown is how many records the list shows.
const lastCallersShown = 15

// postLastCaller posts u's call to the InterBBS Last Callers echo, if
// taking part (config interbbs.last_callers). The record goes out
// under the first sysop's account, as "ibbslastcall" like the other
// boards' do; its time is on the sysop's clock. Failures are logged
// only -- the caller is gone by now anyway.
func (s *Server) postLastCaller(u *user.User) {
	lc := s.LastCallers
	if !lc.Enabled || s.Messages == nil || s.Users == nil {
		return
	}
	area, err := s.Messages.AreaByTag(lc.AreaTag())
	if err != nil {
		s.logWarn("last callers: area %s: %v", lc.AreaTag(), err)
		return
	}
	poster, err := s.Users.FirstSysop()
	if err != nil {
		s.logWarn("last callers: no sysop account to post under: %v", err)
		return
	}
	now := time.Now().In(poster.Location())
	rec := lastcallers.Record{
		Alias:    u.Username,
		BBS:      s.BBSName,
		Date:     lastcallers.FormatDate(now),
		Time:     lastcallers.FormatTime(now),
		Location: callerPlace(u),
		System:   lc.SystemName(),
		Address:  lc.Address,
	}
	// Messages are stored as CP437, like everything Telnet shows.
	body := string(ansi.EncodeCP437(lastcallers.Encode(rec)))
	if _, err := s.Messages.PostMessageAs(area.ID, poster.ID, lastcallers.From, "All", lastcallers.Subject, body); err != nil {
		s.logWarn("last callers: posting %s's call: %v", u.Username, err)
	}
}

// showLastCallers lists the newest InterBBS last callers -- after
// login (show_at_login), and as the menu builtin "lastcallers".
func (s *Server) showLastCallers(term *Terminal, u *user.User) error {
	recs, err := lastcallers.Recent(s.Messages, s.LastCallers.AreaTag(), lastCallersShown)
	if err != nil {
		s.logWarn("last callers: %v", err)
		return nil
	}
	if len(recs) == 0 {
		return nil
	}
	cut := func(v string, n int) string {
		r := []rune(v)
		if len(r) > n {
			r = r[:n]
		}
		return string(r) + strings.Repeat(" ", n-len(r))
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + "  " + term.T("common.interbbs_last_callers") + ansi.Reset + "\r\n\r\n")
	b.WriteString(ansi.FG(ansi.Blue, true) + "  " + padCP(term.T("common.caller"), 16) + padCP(term.T("common.bbs"), 26) + padCP(term.T("common.when"), 16) + term.T("common.from_2") + ansi.Reset + "\r\n")
	b.WriteString(ansi.FG(ansi.Blue, false) + "  " + strings.Repeat("\xc4", 76) + ansi.Reset + "\r\n")
	for _, r := range recs {
		b.WriteString(fmt.Sprintf("  %s%s%s%s%s%s%s%s\r\n",
			ansi.FG(ansi.White, true), cut(r.Alias, 16),
			ansi.FG(ansi.Yellow, true), cut(r.BBS, 26),
			ansi.FG(ansi.White, false), cut(r.Date+" "+r.Time, 16),
			ansi.FG(ansi.Cyan, false), cut(r.Location, 18)+ansi.Reset))
	}
	if err := term.Print(b.String()); err != nil {
		return err
	}
	return s.pauseForKey(term)
}

// callerPlace is u's place for a record: as set in the profile, else
// the city of the profile's time zone, else nothing.
func callerPlace(u *user.User) string {
	if u.Place != "" {
		return u.Place
	}
	return lastcallers.PlaceFromTimezone(u.Timezone)
}
