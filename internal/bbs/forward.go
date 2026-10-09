package bbs

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/midrei/nullmodem-bbs/internal/emailgw"
	"github.com/midrei/nullmodem-bbs/internal/user"
	"github.com/midrei/nullmodem-kit/ansi"
)

// mayForward reports whether u may have their netmail forwarded to an
// address of theirs (see internal/emailgw's forward.go).
func (s *Server) mayForward(u *user.User) bool {
	return s.Forwards != nil && s.mayEmail(u)
}

// forwardLabel is the profile overview's line for u's forwarding.
func (s *Server) forwardLabel(term *Terminal, u *user.User) string {
	fw, err := s.Forwards.Get(u.ID)
	switch {
	case err != nil || fw == nil:
		return term.T("common.off")
	case fw.Pending:
		return term.T("profile.forward_pending", "ADDRESS", fw.Address)
	case fw.Verified:
		return fw.Address
	}
	return term.T("common.off")
}

// sentence is an API error text (they start lower case) as a sentence.
func sentence(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:] + "."
}

// forwardError tells the caller what went wrong.
func (s *Server) forwardError(term *Terminal, err error) error {
	key := ""
	switch {
	case errors.Is(err, emailgw.ErrForwardAddress):
		key = "api.forward_not_address"
	case errors.Is(err, emailgw.ErrForwardOwnDomain):
		key = "api.forward_own_domain"
	case errors.Is(err, emailgw.ErrCodeTooSoon):
		key = "api.forward_too_soon"
	case errors.Is(err, emailgw.ErrCodeWrong):
		key = "api.forward_code_wrong"
	case errors.Is(err, emailgw.ErrCodeExpired):
		key = "api.forward_code_expired"
	case errors.Is(err, emailgw.ErrNoCode):
		key = "api.forward_no_code"
	default:
		key = "api.forward_send_failed"
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + sentence(term.T(key)) + ansi.Reset)
}

// prompt asks a line, the answer in yellow.
func prompt(term *Terminal, text string) (string, error) {
	if err := term.Print(ansi.Reset + text + ansi.FG(ansi.Yellow, true)); err != nil {
		return "", err
	}
	line, err := term.ReadLine(false)
	term.Print(ansi.Reset)
	return strings.TrimSpace(line), err
}

// netmailForward sets up, changes or ends u's netmail forwarding.
func (s *Server) netmailForward(term *Terminal, u *user.User) error {
	for {
		fw, err := s.Forwards.Get(u.ID)
		if err != nil {
			return err
		}
		switch {
		case fw != nil && fw.Pending:
			done, err := s.forwardCode(term, u, fw)
			if err != nil || done {
				return err
			}
		case fw != nil && fw.Verified:
			done, err := s.forwardMenu(term, u, fw)
			if err != nil || done {
				return err
			}
		default:
			sent, err := s.forwardAddress(term, u)
			if err != nil || !sent {
				return err
			}
		}
	}
}

// forwardAddress asks for the address and mails it a code; sent
// reports whether one went.
func (s *Server) forwardAddress(term *Terminal, u *user.User) (sent bool, err error) {
	addr, err := prompt(term, term.T("profile.forward_address_prompt"))
	if err != nil {
		return false, err
	}
	if addr == "" {
		return false, term.Println(term.T("common.cancelled"))
	}
	term.Print(fgDim(ansi.White) + term.T("profile.forward_sending") + ansi.Reset + "\r\n")
	if err := s.Forwards.Request(s.emailConfig(), u, addr, term.Lang, s.BBSName, time.Now()); err != nil {
		if !errors.Is(err, emailgw.ErrForwardAddress) && !errors.Is(err, emailgw.ErrForwardOwnDomain) && !errors.Is(err, emailgw.ErrCodeTooSoon) {
			s.logWarn("%s: forwarding code not sent: %v", u.Username, err)
		}
		return false, s.forwardError(term, err)
	}
	s.logInfo("%s asked for netmail forwarding to an address of theirs", u.Username)
	return true, term.Println(ansi.FG(ansi.Green, true) + term.T("profile.forward_code_sent", "ADDRESS", addr) + ansi.Reset)
}

// forwardCode asks for the code mailed to fw.Address; done: back to
// the profile.
func (s *Server) forwardCode(term *Terminal, u *user.User, fw *emailgw.Forward) (done bool, err error) {
	code, err := prompt(term, term.T("profile.forward_code_prompt"))
	if err != nil {
		return true, err
	}
	switch strings.ToUpper(code) {
	case "":
		return true, nil // later: the code stays valid for a while
	case "N":
		term.Print(fgDim(ansi.White) + term.T("profile.forward_sending") + ansi.Reset + "\r\n")
		if err := s.Forwards.Request(s.emailConfig(), u, fw.Address, term.Lang, s.BBSName, time.Now()); err != nil {
			return false, s.forwardError(term, err)
		}
		return false, term.Println(ansi.FG(ansi.Green, true) + term.T("profile.forward_code_sent", "ADDRESS", fw.Address) + ansi.Reset)
	case "X":
		if err := s.Forwards.Remove(u.ID); err != nil {
			return true, err
		}
		return true, term.Println(term.T("common.cancelled"))
	}
	if err := s.Forwards.Confirm(u.ID, code, time.Now()); err != nil {
		if errors.Is(err, emailgw.ErrCodeWrong) {
			return false, s.forwardError(term, err)
		}
		return true, s.forwardError(term, err)
	}
	s.logInfo("%s turned on netmail forwarding", u.Username)
	return true, term.Println(ansi.FG(ansi.Green, true) + term.T("profile.forward_on", "ADDRESS", fw.Address) + ansi.Reset)
}

// forwardMenu is the forwarding that's on: mark read, another
// address, off.
func (s *Server) forwardMenu(term *Terminal, u *user.User, fw *emailgw.Forward) (done bool, err error) {
	var b strings.Builder
	b.WriteString("\r\n  " + ansi.FG(ansi.White, true) + term.T("web.profile.forward_active", "ADDRESS", fw.Address) + ansi.Reset + "\r\n\r\n")
	b.WriteString("  " + profileOption("R", padCP(term.T("profile.forward_mark_read", "ONOFF", onOffText(term, fw.MarkRead)), 30)))
	b.WriteString(profileOption("N", term.T("web.profile.forward_change")) + ansi.Reset + "\r\n")
	b.WriteString("  " + profileOption("X", padCP(term.T("web.profile.forward_stop"), 30)))
	b.WriteString(profileOption("Q", term.T("common.back")) + ansi.Reset + "\r\n")
	if err := term.Print(b.String()); err != nil {
		return true, err
	}
	choice, err := prompt(term, "\r\n"+fgDim(ansi.White)+term.T("common.choice")+" ")
	if err != nil {
		return true, err
	}
	switch strings.ToUpper(choice) {
	case "R":
		if err := s.Forwards.SetMarkRead(u.ID, !fw.MarkRead); err != nil {
			return true, err
		}
		return false, nil
	case "N":
		sent, err := s.forwardAddress(term, u)
		return !sent, err
	case "X":
		if err := s.Forwards.Remove(u.ID); err != nil {
			return true, err
		}
		s.logInfo("%s turned off netmail forwarding", u.Username)
		return true, term.Println(ansi.FG(ansi.Green, true) + term.T("web.profile.forward_off") + ansi.Reset)
	case "Q", "":
		return true, nil
	}
	return false, term.Println(ansi.FG(ansi.Red, true) + term.T("common.unknown_choice") + ansi.Reset)
}
