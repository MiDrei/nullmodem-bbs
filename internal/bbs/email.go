package bbs

import (
	"errors"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/emailgw"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
)

// Netmail to an email address goes out through the email gateway
// (internal/emailgw, run by the web daemon): it's stored here, the
// gateway sends it.

func (s *Server) emailConfig() config.EmailConfig {
	if s.Email == nil {
		return config.EmailConfig{}
	}
	return s.Email()
}

// mayEmail reports whether u may write email at all.
func (s *Server) mayEmail(u *user.User) bool {
	return emailgw.May(s.emailConfig(), u)
}

// emailAllowed says why u can't send a mail now, if so.
func (s *Server) emailAllowed(term *Terminal, u *user.User) (bool, error) {
	cfg := s.emailConfig()
	err := emailgw.CheckSend(cfg, s.Netmail, u, time.Now())
	var key string
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, emailgw.ErrOff):
		key = "netmail.email_off"
	case errors.Is(err, emailgw.ErrNotAllowed):
		key = "netmail.email_not_allowed"
	case errors.Is(err, emailgw.ErrLimit):
		return false, term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("netmail.email_limit", "LIMIT", cfg.Limit()) + ansi.Reset)
	default:
		return false, err
	}
	return false, term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T(key) + ansi.Reset)
}

// composeEmail writes a new mail to the address to.
func (s *Server) composeEmail(term *Terminal, u *user.User, to string) error {
	if ok, err := s.emailAllowed(term, u); !ok {
		return err
	}
	if err := term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "  -> " +
		term.T("netmail.email_from", "ADDRESS", emailgw.Address(s.emailConfig(), u.Username)) + ansi.Reset); err != nil {
		return err
	}
	if err := term.Print(ansi.Reset + term.T("msg.subject") + " " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	subject, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return term.Println(ansi.Reset + term.T("common.cancelled"))
	}
	lines, saved, err := s.runEditor(term, editorHeader(term, term.T("common.email"), to, subject), nil, u.LineEditor)
	if err != nil {
		return err
	}
	if !saved {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("msg.aborted"))
	}
	_, err = s.sendEmail(term, u, to, subject, lines, 0)
	return err
}

// sendEmail stores the mail for the gateway and says so; replyTo is
// the netmail it answers (0: none).
func (s *Server) sendEmail(term *Terminal, u *user.User, to, subject string, lines []string, replyTo int64) (bool, error) {
	if ok, err := s.emailAllowed(term, u); !ok {
		return false, err
	}
	if _, err := s.Netmail.SendEmail(u.ID, s.FTNAddress, to, subject, strings.Join(lines, "\n"), replyTo); err != nil {
		return false, err
	}
	s.logInfo("%s wrote an email to %s", u.Username, to)
	return true, term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("netmail.email_queued", "TO", to) + ansi.Reset)
}
