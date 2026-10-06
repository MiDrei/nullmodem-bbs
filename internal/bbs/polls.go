package bbs

import (
	"fmt"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/community"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// The voting booth: the sysop's polls (made in the web admin), voted
// on here -- one vote each, which may be changed -- with the results
// as bars once voted.

func (s *Server) votingBooth(term *Terminal, u *user.User) error {
	if s.Community == nil {
		return nil
	}
	for {
		polls, err := s.Community.Polls(u.ID, false)
		if err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString(s.featureHeader(term, u, "polls.ans", term.T("common.voting_booth")))
		if len(polls) == 0 {
			b.WriteString("  " + term.T("polls.none") + "\r\n")
			if err := term.Print(b.String()); err != nil {
				return err
			}
			return s.pauseForKey(term)
		}
		for i, p := range polls {
			mark := ansi.FG(ansi.Cyan, true) + " " + term.T("common.new_3") + ansi.Reset
			if p.MyVote != 0 {
				mark = ansi.FG(ansi.Green, false) + " " + term.T("polls.voted") + ansi.Reset
			}
			fmt.Fprintf(&b, "  %s%2d%s  %s%s  %s(%s)%s\r\n", ansi.FG(ansi.Cyan, true), i+1, ansi.FG(ansi.White, true),
				toCP437(p.Question), mark, ansi.FG(ansi.Black, true), term.N("common.count_votes", p.Total), ansi.Reset)
		}
		b.WriteString("\r\n  " + term.T("polls.which") + " " + ansi.FG(ansi.Yellow, true))
		if err := term.Print(b.String()); err != nil {
			return err
		}
		in, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		in = strings.TrimSpace(in)
		if in == "" {
			return term.Print(ansi.Reset)
		}
		n, err := strconv.Atoi(in)
		if err != nil || n < 1 || n > len(polls) {
			continue
		}
		if err := s.showPoll(term, u, polls[n-1]); err != nil {
			return err
		}
	}
}

// showPoll asks for a vote (or a changed one) and shows the results.
func (s *Server) showPoll(term *Terminal, u *user.User, p community.Poll) error {
	var b strings.Builder
	b.WriteString(s.featureHeader(term, u, "polls.ans", term.T("common.voting_booth")))
	b.WriteString("  " + ansi.FG(ansi.White, true) + toCP437(p.Question) + ansi.Reset + "\r\n\r\n")
	for i, o := range p.Options {
		chosen := "  "
		if o.ID == p.MyVote {
			chosen = ansi.FG(ansi.Green, true) + " *" + ansi.Reset
		}
		fmt.Fprintf(&b, "  %s%s%2d%s  %s\r\n", chosen, ansi.FG(ansi.Cyan, true), i+1, ansi.Reset, toCP437(o.Text))
	}
	if !u.Validated {
		b.WriteString("\r\n  " + ansi.FG(ansi.Yellow, false) + term.T("polls.not_approved") + ansi.Reset + "\r\n")
		if err := term.Print(b.String()); err != nil {
			return err
		}
		return s.showResults(term, p)
	}
	prompt := term.T("polls.your_vote")
	if p.MyVote != 0 {
		prompt = term.T("polls.change_vote")
	}
	b.WriteString("\r\n  " + prompt + " " + term.T("polls.vote_hint") + " " + ansi.FG(ansi.Yellow, true))
	if err := term.Print(b.String()); err != nil {
		return err
	}
	in, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	if n, err := strconv.Atoi(strings.TrimSpace(in)); err == nil && n >= 1 && n <= len(p.Options) {
		if err := s.Community.Vote(p.ID, u.ID, p.Options[n-1].ID); err != nil {
			return err
		}
		if p, err = s.Community.Poll(p.ID, u.ID); err != nil {
			return err
		}
	}
	return s.showResults(term, p)
}

// showResults draws the votes as bars.
func (s *Server) showResults(term *Terminal, p community.Poll) error {
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n  " + ansi.FG(ansi.Cyan, true) + term.T("polls.results") + ansi.Reset + " (" + term.N("common.count_votes", p.Total) + ")\r\n")
	const width = 30
	for _, o := range p.Options {
		pct := 0
		if p.Total > 0 {
			pct = o.Votes * 100 / p.Total
		}
		bar := o.Votes * width / max(1, p.Total)
		name := []rune(o.Text)
		if len(name) > 24 {
			name = name[:24]
		}
		color := ansi.FG(ansi.Blue, true)
		if o.ID == p.MyVote {
			color = ansi.FG(ansi.Green, true)
		}
		fmt.Fprintf(&b, "  %-24s %s%s%s%s %3d%% (%d)\r\n", toCP437(string(name)), color, strings.Repeat("\xdb", bar),
			ansi.FG(ansi.Black, true)+strings.Repeat("\xb0", width-bar), ansi.Reset, pct, o.Votes)
	}
	if err := term.Print(b.String()); err != nil {
		return err
	}
	return s.pauseForKey(term)
}
