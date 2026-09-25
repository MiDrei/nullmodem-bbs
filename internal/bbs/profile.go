package bbs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
)

// commonTimezones is the short list the Telnet/SSH time zone picker
// offers by number; any other IANA name can still be typed in via
// "Other". The web portal offers the browser's full IANA list instead.
var commonTimezones = []string{
	"UTC",
	"Europe/London",
	"Europe/Lisbon",
	"Europe/Zurich",
	"Europe/Berlin",
	"Europe/Vienna",
	"Europe/Paris",
	"Europe/Amsterdam",
	"Europe/Stockholm",
	"Europe/Helsinki",
	"Europe/Athens",
	"Europe/Moscow",
	"America/New_York",
	"America/Chicago",
	"America/Denver",
	"America/Los_Angeles",
	"America/Sao_Paulo",
	"Asia/Kolkata",
	"Asia/Tokyo",
	"Australia/Sydney",
	"Pacific/Auckland",
}

// realNameErrorText is the caller-facing wording for
// user.ValidateRealName's errors.
func realNameErrorText(err error) string {
	switch {
	case errors.Is(err, user.ErrRealNameRequired):
		return "Real name is required."
	case errors.Is(err, user.ErrRealNameReserved):
		return "That name is reserved."
	default:
		return err.Error()
	}
}

// utcOffsetLabel renders loc's current offset from UTC, e.g.
// "UTC+02:00", for the time zone picker and profile overview.
func utcOffsetLabel(loc *time.Location) string {
	_, offset := time.Now().In(loc).Zone()
	sign := '+'
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	return fmt.Sprintf("UTC%c%02d:%02d", sign, offset/3600, offset%3600/60)
}

// timezoneLabel describes u's profile time zone for display.
func timezoneLabel(u *user.User) string {
	if u.Timezone == "" {
		return "not set (times shown in UTC)"
	}
	return fmt.Sprintf("%s (%s)", u.Timezone, utcOffsetLabel(u.Location()))
}

// showProfile is the "builtin:profile" command (also reachable as the
// older "builtin:stats"): the caller's account overview plus the
// settings they can change themselves -- real name, time zone,
// password, and QWK area selection. The web portal's /profile page
// offers exactly the same (see internal/web's bbs_profile_handler.go).
// Changes are written through to u, so the rest of the session sees
// them immediately.
func (s *Server) showProfile(term *Terminal, u *user.User) error {
	for {
		lines := []string{
			ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + "Your profile" + ansi.Reset,
			fmt.Sprintf("Handle:         %s", u.Username),
			fmt.Sprintf("Real name:      %s", u.RealName),
			fmt.Sprintf("Security level: %d", u.SecurityLevel),
			fmt.Sprintf("Total calls:    %d", u.TotalCalls),
			fmt.Sprintf("Member since:   %s", term.Time(u.CreatedAt).Format("2006-01-02")),
			fmt.Sprintf("Time zone:      %s", timezoneLabel(u)),
			fmt.Sprintf("QWK SEEN-BY:    %s", onOff(u.QWKRouting)),
			"",
			profileOption("R", "Change real name"),
			profileOption("T", "Change time zone"),
			profileOption("P", "Change password"),
			profileOption("K", "QWK area selection"),
			profileOption("S", "Switch SEEN-BY/PATH lines in QWK packets on or off"),
			profileOption("Q", "Back"),
		}
		for _, line := range lines {
			if err := term.Println(line); err != nil {
				return err
			}
		}
		if err := term.Print("\nChoice: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		choice, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		if err := term.Print(ansi.Reset); err != nil {
			return err
		}

		switch strings.ToUpper(strings.TrimSpace(choice)) {
		case "R":
			err = s.changeRealName(term, u)
		case "T":
			err = s.changeTimezone(term, u)
		case "P":
			err = s.changePassword(term, u)
		case "K":
			err = s.configureQWKAreas(term, u)
		case "S":
			err = s.toggleQWKRouting(term, u)
		case "Q", "":
			return nil
		default:
			err = term.Println(ansi.FG(ansi.Red, true) + "Unknown choice." + ansi.Reset)
		}
		if err != nil {
			return err
		}
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// toggleQWKRouting switches whether this caller's QWK packets carry
// echomail's SEEN-BY/PATH lines (see user.User.QWKRouting).
func (s *Server) toggleQWKRouting(term *Terminal, u *user.User) error {
	if err := s.Users.SetQWKRouting(u.ID, !u.QWKRouting); err != nil {
		return err
	}
	u.QWKRouting = !u.QWKRouting
	s.logInfo("%s turned QWK SEEN-BY/PATH lines %s", u.Username, onOff(u.QWKRouting))
	msg := "QWK packets now leave SEEN-BY/PATH out."
	if u.QWKRouting {
		msg = "QWK packets now carry SEEN-BY/PATH -- for a reader that hides them, like NullModem Reader."
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + msg)
}

func profileOption(key, label string) string {
	return fmt.Sprintf("  [%s%s%s] %s", ansi.FG(ansi.Yellow, true), key, ansi.Reset, label)
}

// changeRealName prompts for a new real name, validated the same way
// as at registration (user.ValidateRealName); an empty line cancels.
func (s *Server) changeRealName(term *Terminal, u *user.User) error {
	for {
		if err := term.Print(ansi.Reset + "New real name (Enter to cancel): " + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		line, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		realName := strings.TrimSpace(line)
		if realName == "" {
			return term.Println(ansi.Reset + "Cancelled.")
		}
		if err := user.ValidateRealName(realName); err != nil {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + realNameErrorText(err)); err != nil {
				return err
			}
			continue
		}
		if err := s.Users.SetRealName(u.ID, realName); err != nil {
			return err
		}
		u.RealName = realName
		s.logInfo("%s changed their real name", u.Username)
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Real name saved.")
	}
}

// changeTimezone offers commonTimezones by number, any other IANA name
// via "O", or "N" to unset it again.
func (s *Server) changeTimezone(term *Terminal, u *user.User) error {
	for {
		var b strings.Builder
		b.WriteString(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + "Time zone" + ansi.Reset + " -- currently " + timezoneLabel(u) + "\n")
		half := (len(commonTimezones) + 1) / 2
		for row := 0; row < half; row++ {
			b.WriteString(timezoneCell(row))
			if right := row + half; right < len(commonTimezones) {
				b.WriteString("  " + timezoneCell(right))
			}
			b.WriteString("\n")
		}
		b.WriteString(" O) Other (any IANA name)   N) Not set   Q) Cancel\n")
		b.WriteString("\nChoice: " + ansi.FG(ansi.Yellow, true))
		if err := term.Print(b.String()); err != nil {
			return err
		}
		line, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		choice := strings.TrimSpace(line)

		var name string
		switch strings.ToUpper(choice) {
		case "", "Q":
			return term.Println(ansi.Reset + "Cancelled.")
		case "N":
			name = ""
		case "O":
			if err := term.Print(ansi.Reset + "IANA zone name, e.g. Asia/Tokyo (case matters): " + ansi.FG(ansi.Yellow, true)); err != nil {
				return err
			}
			typed, err := term.ReadLine(false)
			if err != nil {
				return err
			}
			name = strings.TrimSpace(typed)
			if name == "" {
				continue
			}
		default:
			n, convErr := strconv.Atoi(choice)
			if convErr != nil || n < 1 || n > len(commonTimezones) {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid selection."); err != nil {
					return err
				}
				continue
			}
			name = commonTimezones[n-1]
		}

		if err := s.Users.SetTimezone(u.ID, name); err != nil {
			if errors.Is(err, user.ErrInvalidTimezone) {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Unknown time zone: " + name); err != nil {
					return err
				}
				continue
			}
			return err
		}
		u.Timezone = name
		term.SetLocation(u.Location())
		s.logInfo("%s set their time zone to %q", u.Username, name)
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Time zone saved: " + timezoneLabel(u))
	}
}

// timezoneCell renders one numbered entry of the time zone picker,
// padded to a fixed width so two columns line up.
func timezoneCell(i int) string {
	name := commonTimezones[i]
	loc, err := time.LoadLocation(name)
	offset := ""
	if err == nil {
		offset = utcOffsetLabel(loc)
	}
	return fmt.Sprintf("%2d) %-19s %-9s", i+1, name, offset)
}

// changePassword verifies the current password, then asks for the new
// one twice -- the same rules as registration (user.MinPasswordLength).
func (s *Server) changePassword(term *Terminal, u *user.User) error {
	if err := term.Print(ansi.Reset + "Current password: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	current, err := term.ReadLine(true)
	if err != nil {
		return err
	}
	if current == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}
	if err := term.Print(ansi.Reset + fmt.Sprintf("New password (min %d chars): ", user.MinPasswordLength) + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	next, err := term.ReadLine(true)
	if err != nil {
		return err
	}
	if err := term.Print(ansi.Reset + "Confirm new password: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	confirm, err := term.ReadLine(true)
	if err != nil {
		return err
	}
	if next != confirm {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Passwords did not match -- nothing changed.")
	}

	switch err := s.Users.ChangePassword(u.ID, current, next); {
	case errors.Is(err, user.ErrInvalidCredentials):
		s.logWarn("%s: failed password change (wrong current password)", u.Username)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Current password is incorrect -- nothing changed.")
	case errors.Is(err, user.ErrPasswordTooShort):
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + fmt.Sprintf("Password too short (min %d chars) -- nothing changed.", user.MinPasswordLength))
	case err != nil:
		return err
	}
	s.logInfo("%s changed their password", u.Username)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Password changed.")
}
