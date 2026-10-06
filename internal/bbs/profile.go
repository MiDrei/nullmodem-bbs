package bbs

import (
	"errors"
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/emailgw"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/i18n"
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
func realNameErrorText(term *Terminal, err error) string {
	switch {
	case errors.Is(err, user.ErrRealNameRequired):
		return term.T("profile.real_name_required")
	case errors.Is(err, user.ErrRealNameReserved):
		return term.T("profile.real_name_reserved")
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
func timezoneLabel(term *Terminal, u *user.User) string {
	if u.Timezone == "" {
		return term.T("profile.timezone_unset")
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
		// The short details in two columns, the ones that may run long
		// (time zone, place, e-mail address) on lines of their own;
		// then the options, three a line.
		left := [][2]string{
			{"common.handle_2", u.Username},
			{"common.real_name", toCP437(u.RealName)},
			{"common.security_level", strconv.Itoa(u.SecurityLevel)},
			{"profile.editor", editorText(term, u)},
		}
		right := [][2]string{
			{"common.total_calls", strconv.Itoa(u.TotalCalls)},
			{"common.member_since", term.Time(u.CreatedAt).Format("2006-01-02")},
			{"common.language", toCP437(i18n.NameOf(term.Lang))},
			{"profile.qwk_seenby", onOffText(term, u.QWKRouting)},
		}
		var b strings.Builder
		b.WriteString(s.featureHeader(term, u, "profile.ans", term.T("common.your_profile")))
		for i := range left {
			b.WriteString("  " + profileField(term, left[i][0], padCP(left[i][1], 22)) + "  " +
				fgDim(ansi.White) + padCP(term.T(right[i][0])+":", 15) + ansi.FG(ansi.White, true) + right[i][1] + ansi.Reset + "\r\n")
		}
		b.WriteString("  " + profileField(term, "common.time_zone", timezoneLabel(term, u)) + ansi.Reset + "\r\n")
		b.WriteString("  " + profileField(term, "common.location", placeLabel(term, u)) + ansi.Reset + "\r\n")
		if s.mayEmail(u) {
			b.WriteString("  " + profileField(term, "common.email", emailgw.Address(s.emailConfig(), u.Username)) + ansi.Reset + "\r\n")
		}
		b.WriteString("\r\n")
		options := [][2]string{
			{"R", term.T("profile.opt_real_name")},
			{"T", term.T("profile.opt_timezone")},
			{"L", term.T("profile.opt_location")},
			{"A", term.T("profile.opt_language")},
			{"P", term.T("common.change_password")},
			{"K", term.T("common.qwk_area_selection")},
			{"S", term.T("profile.opt_seenby")},
			{"E", term.T("profile.opt_editor")},
			{"Q", term.T("common.back")},
		}
		for i, o := range options {
			if i%3 == 0 {
				b.WriteString("  ")
			}
			b.WriteString(profileOption(o[0], padCP(o[1], 22)))
			if i%3 == 2 || i == len(options)-1 {
				b.WriteString(ansi.Reset + "\r\n")
			}
		}
		if err := term.Print(b.String()); err != nil {
			return err
		}
		if err := term.Print("\r\n" + fgDim(ansi.White) + term.T("common.choice") + " " + ansi.FG(ansi.Yellow, true)); err != nil {
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
		case "L":
			err = s.changePlace(term, u)
		case "A":
			err = s.changeLanguage(term, u)
		case "P":
			err = s.changePassword(term, u)
		case "K":
			err = s.configureQWKAreas(term, u)
		case "S":
			err = s.toggleQWKRouting(term, u)
		case "E":
			err = s.toggleLineEditor(term, u)
		case "Q", "":
			return nil
		default:
			err = term.Println(ansi.FG(ansi.Red, true) + term.T("common.unknown_choice") + ansi.Reset)
		}
		if err != nil {
			return err
		}
	}
}

// profileField is a "Label:   value" line of the overview, the values
// lined up.
func profileField(term *Terminal, key, value string) string {
	return fgDim(ansi.White) + padCP(term.T(key)+":", 16) + ansi.FG(ansi.White, true) + value
}

// onOffText is on/off in the caller's language.
func onOffText(term *Terminal, b bool) string {
	if b {
		return term.T("common.on")
	}
	return term.T("common.off")
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
	msg := term.T("common.qwk_packets_now_leave_seen")
	if u.QWKRouting {
		msg = term.T("profile.seenby_on")
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + msg)
}

// editorText is editorLabel in the caller's language.
func editorText(term *Terminal, u *user.User) string {
	if u.LineEditor {
		return term.T("profile.editor_line")
	}
	return term.T("profile.editor_full")
}

func editorLabel(u *user.User) string {
	if u.LineEditor {
		return "line by line"
	}
	return "full screen"
}

// toggleLineEditor switches between the full-screen editor and the
// line editor (user.User.LineEditor).
func (s *Server) toggleLineEditor(term *Terminal, u *user.User) error {
	if err := s.Users.SetLineEditor(u.ID, !u.LineEditor); err != nil {
		return err
	}
	u.LineEditor = !u.LineEditor
	s.logInfo("%s now writes %s", u.Username, editorLabel(u))
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("profile.editor_now", "EDITOR", editorText(term, u)))
}

func profileOption(key, label string) string {
	return ansi.FG(ansi.Black, true) + "[" + ansi.FG(ansi.Cyan, true) + key + ansi.FG(ansi.Black, true) + "] " + ansi.FG(ansi.White, true) + label
}

// changeRealName prompts for a new real name, validated the same way
// as at registration (user.ValidateRealName); an empty line cancels.
func (s *Server) changeRealName(term *Terminal, u *user.User) error {
	for {
		if err := term.Print(ansi.Reset + term.T("profile.new_real_name") + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		line, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		realName := strings.TrimSpace(line)
		if realName == "" {
			return term.Println(ansi.Reset + term.T("common.cancelled"))
		}
		if err := user.ValidateRealName(realName); err != nil {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + realNameErrorText(term, err)); err != nil {
				return err
			}
			continue
		}
		if err := s.Users.SetRealName(u.ID, realName); err != nil {
			return err
		}
		u.RealName = realName
		s.logInfo("%s changed their real name", u.Username)
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("common.real_name_saved"))
	}
}

// changeTimezone offers commonTimezones by number, any other IANA name
// via "O", or "N" to unset it again.
func (s *Server) changeTimezone(term *Terminal, u *user.User) error {
	for {
		var b strings.Builder
		b.WriteString(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + term.T("common.time_zone") + ansi.Reset + " -- " + term.T("profile.tz_current", "ZONE", timezoneLabel(term, u)) + "\n")
		half := (len(commonTimezones) + 1) / 2
		for row := 0; row < half; row++ {
			b.WriteString(timezoneCell(row))
			if right := row + half; right < len(commonTimezones) {
				b.WriteString("  " + timezoneCell(right))
			}
			b.WriteString("\n")
		}
		b.WriteString(" " + term.T("profile.tz_options") + "\n")
		b.WriteString("\n" + term.T("common.choice") + " " + ansi.FG(ansi.Yellow, true))
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
			return term.Println(ansi.Reset + term.T("common.cancelled"))
		case "N":
			name = ""
		case "O":
			if err := term.Print(ansi.Reset + term.T("profile.tz_other") + ansi.FG(ansi.Yellow, true)); err != nil {
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
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("common.invalid_selection")); err != nil {
					return err
				}
				continue
			}
			name = commonTimezones[n-1]
		}

		if err := s.Users.SetTimezone(u.ID, name); err != nil {
			if errors.Is(err, user.ErrInvalidTimezone) {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("profile.tz_unknown", "ZONE", name)); err != nil {
					return err
				}
				continue
			}
			return err
		}
		u.Timezone = name
		term.SetLocation(u.Location())
		s.logInfo("%s set their time zone to %q", u.Username, name)
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("profile.tz_saved", "ZONE", timezoneLabel(term, u)))
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
	if err := term.Print(ansi.Reset + term.T("profile.pw_current") + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	current, err := term.ReadLine(true)
	if err != nil {
		return err
	}
	if current == "" {
		return term.Println(ansi.Reset + term.T("common.cancelled"))
	}
	if err := term.Print(ansi.Reset + term.T("profile.pw_new", "MIN", user.MinPasswordLength) + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	next, err := term.ReadLine(true)
	if err != nil {
		return err
	}
	if err := term.Print(ansi.Reset + term.T("profile.pw_confirm") + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	confirm, err := term.ReadLine(true)
	if err != nil {
		return err
	}
	if next != confirm {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("profile.pw_mismatch"))
	}

	switch err := s.Users.ChangePassword(u.ID, current, next); {
	case errors.Is(err, user.ErrInvalidCredentials):
		s.logWarn("%s: failed password change (wrong current password)", u.Username)
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("profile.pw_wrong"))
	case errors.Is(err, user.ErrPasswordTooShort):
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("profile.pw_short", "MIN", user.MinPasswordLength))
	case err != nil:
		return err
	}
	s.logInfo("%s changed their password", u.Username)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("common.password_changed"))
}

func placeLabel(term *Terminal, u *user.User) string {
	if u.Place == "" {
		return term.T("common.not_set")
	}
	return u.Place
}

// changePlace asks where the caller is ("City, Country"); "-" clears
// it, an empty line cancels.
func (s *Server) changePlace(term *Terminal, u *user.User) error {
	for {
		if err := term.Print(ansi.Reset + term.T("profile.location_prompt", "MAX", user.MaxPlaceLen) + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}
		line, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return term.Println(ansi.Reset + term.T("common.cancelled"))
		}
		if line == "-" {
			line = ""
		}
		place, ok := user.CleanPlace(line)
		if !ok {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("common.at_most_chars", "MAX", user.MaxPlaceLen)); err != nil {
				return err
			}
			continue
		}
		if err := s.Users.SetPlace(u.ID, place); err != nil {
			return err
		}
		u.Place = place
		s.logInfo("%s set their location", u.Username)
		return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("common.location_saved"))
	}
}
