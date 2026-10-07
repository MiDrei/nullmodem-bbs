package bbs

import (
	"strings"
	"time"

	"github.com/midrei/nullmodem-kit/ansi"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

// The sysop functions over Telnet/SSH (the sysop menu and its
// commands) are as powerful as parts of the web admin -- setsl makes
// any account a sysop -- so they ask for the same second factor: the
// code from the authenticator app, once per call. With the BBS
// requiring two-factor login for the admin, a sysop without it is
// turned away here too.

// sysopBuiltins are the commands only the sysop menu offers.
var sysopBuiltins = map[string]bool{
	"listusers": true, "setsl": true, "createarea": true, "createfilearea": true, "importfile": true,
}

func (s *Server) sysopGate(term *Terminal, u *user.User) (bool, error) {
	if term.sysopOK {
		return true, nil
	}
	if !u.TwoFactor {
		if s.Security != nil && s.Security().RequireAdminTOTP {
			return false, term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Red, true) +
				term.T("sysop.need_2fa") + ansi.Reset)
		}
		term.sysopOK = true
		return true, nil
	}
	if err := term.Print(ansi.Reset + "\n" + term.T("sysop.code_prompt") + ansi.FG(ansi.Yellow, true)); err != nil {
		return false, err
	}
	code, err := term.ReadLine(false)
	if err != nil {
		return false, err
	}
	term.Print(ansi.Reset)
	if err := s.Users.VerifyTOTP(u.ID, strings.TrimSpace(code), time.Now()); err != nil {
		s.logWarn("[%s] wrong two-factor code for %s's sysop functions", term.Protocol, u.Username)
		if s.Guard != nil {
			if v, err := s.Guard.Fail(term.RemoteIP, u.Username, term.Protocol+" 2fa"); err == nil && v.Blocked {
				term.Println(ansi.FG(ansi.Red, true) + toCP437(v.MessageIn(term.Lang)) + ansi.Reset)
				return false, errLogoff
			}
		}
		return false, term.Println(ansi.FG(ansi.Red, true) + term.T("sysop.wrong_code") + ansi.Reset)
	}
	term.sysopOK = true
	return true, nil
}
