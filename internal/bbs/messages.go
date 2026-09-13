package bbs

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// postBodyTerminator is the sentinel a caller types on its own line to
// finish composing a message, matching classic BBS message editors.
const postBodyTerminator = "."

// showAreas is the "builtin:areas" command: a lightbar over every
// message area the caller can read, showing each area's Total/New/
// Yours message counts and letting them move the highlighted row
// with the arrow keys, Enter to browse that area, Q/Escape to
// return.
func (s *Server) showAreas(term *Terminal, u *user.User) error {
	selected := 0
outer:
	for {
		stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
		if err != nil {
			return err
		}
		if len(stats) == 0 {
			if err := s.printAreaHeader(term, u, "msgareas.ans", "Message Areas"); err != nil {
				return err
			}
			return term.Println(ansi.Reset + "\nNo message areas available.")
		}
		if selected >= len(stats) {
			selected = len(stats) - 1
		}

		for {
			if err := s.drawAreaLightbar(term, u, stats, selected); err != nil {
				return err
			}
			key, err := term.ReadKey()
			if err != nil {
				return err
			}
			switch {
			case key.Type == KeyUp:
				selected = (selected - 1 + len(stats)) % len(stats)
			case key.Type == KeyDown:
				selected = (selected + 1) % len(stats)
			case key.Type == KeyEnter:
				area := stats[selected].Area
				if err := s.Messages.MarkAreaRead(u.ID, area.ID); err != nil {
					return err
				}
				if err := s.browseArea(term, u, &area); err != nil {
					return err
				}
				continue outer
			case key.Type == KeyEscape:
				return nil
			case key.Type == KeyChar && (key.Rune == 'q' || key.Rune == 'Q'):
				return nil
			}
		}
	}
}

// Fixed filenames for the hand-designed pieces of the area lightbar,
// beyond the banner (see printAreaHeader): a plain, uncustomized
// fallback covers each one so a missing or deleted file degrades to
// a working (if plain) table instead of breaking the listing.
const (
	msgAreaColumnsScreen     = "msgareas-columns.ans"
	msgAreaRowScreen         = "msgareas-row.ans"
	msgAreaRowSelectedScreen = "msgareas-row-selected.ans"
)

const (
	fallbackAreaRow         = "{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}"
	fallbackAreaRowSelected = "\x1b[47m\x1b[30m{AREANAME:-58} {TOTAL:6} {NEW:6} {YOURS:6}\x1b[0m"
)

var fallbackAreaColumns = "Area                                                           Total    New  Yours\r\n" + strings.Repeat("-", 79)

// loadOptionalScreen returns screenFile's raw content from ScreensDir,
// or fallback if it doesn't exist / can't be read.
func (s *Server) loadOptionalScreen(screenFile, fallback string) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, screenFile))
	if err != nil {
		return fallback
	}
	return raw
}

// drawAreaLightbar redraws the message-area header banner plus the
// Total/New/Yours table, with the row at selected highlighted. The
// column header and the two row styles (normal/selected) are each
// their own hand-designed screen file, rendered with the AREANAME/
// TOTAL/NEW/YOURS/NEWFLAG placeholders -- see internal/ansi.Render's
// {NAME:WIDTH} form, which is what keeps the numeric columns aligned
// even though area names vary in length.
func (s *Server) drawAreaLightbar(term *Terminal, u *user.User, stats []message.AreaWithStats, selected int) error {
	if err := s.printAreaHeader(term, u, "msgareas.ans", "Message Areas"); err != nil {
		return err
	}

	rowTemplate := s.loadOptionalScreen(msgAreaRowScreen, fallbackAreaRow)
	rowSelectedTemplate := s.loadOptionalScreen(msgAreaRowSelectedScreen, fallbackAreaRowSelected)

	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	b.WriteString(s.loadOptionalScreen(msgAreaColumnsScreen, fallbackAreaColumns))
	b.WriteString(ansi.CRLF)

	for i, st := range stats {
		tmpl := rowTemplate
		if i == selected {
			tmpl = rowSelectedTemplate
		}
		newFlag := ""
		if st.New > 0 {
			newFlag = "NEW"
		}
		vars := ansi.Vars{
			"AREANAME": st.Area.Name,
			"TOTAL":    strconv.Itoa(st.Total),
			"NEW":      strconv.Itoa(st.New),
			"YOURS":    strconv.Itoa(st.Yours),
			"NEWFLAG":  newFlag,
		}
		b.WriteString(ansi.Render(tmpl, vars))
		b.WriteString(ansi.CRLF)
	}
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.White, true) + "[Up/Down] Move   [Enter] Select   [Q] Back" + ansi.Reset)
	return term.Print(b.String())
}

// browseArea lists an area's messages and lets the caller read one,
// post a new one (if their SL allows), or return to the area list.
func (s *Server) browseArea(term *Terminal, u *user.User, area *message.Area) error {
	for {
		msgs, err := s.Messages.ListMessages(area.ID)
		if err != nil {
			return err
		}

		if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + area.Name + ansi.Reset); err != nil {
			return err
		}
		if len(msgs) == 0 {
			if err := term.Println("(no messages yet)"); err != nil {
				return err
			}
		}
		for i, m := range msgs {
			line := fmt.Sprintf("%3d) %-30s %-15s %s", i+1, m.Subject, m.FromName, m.PostedAt.Format("2006-01-02 15:04"))
			if err := term.Println(line); err != nil {
				return err
			}
		}

		canWrite := area.CanWrite(u.SecurityLevel)
		prompt := "\nMessage # to read"
		if canWrite {
			prompt += ", [P]ost"
		}
		prompt += ", [Q]uit: "
		if err := term.Print(ansi.Reset + prompt + ansi.FG(ansi.Yellow, true)); err != nil {
			return err
		}

		choice, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		choice = strings.TrimSpace(choice)

		switch {
		case choice == "" || strings.EqualFold(choice, "Q"):
			return nil

		case strings.EqualFold(choice, "P"):
			if !canWrite {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "You don't have permission to post here."); err != nil {
					return err
				}
				continue
			}
			if err := s.postMessage(term, u, area); err != nil {
				return err
			}

		default:
			idx, convErr := strconv.Atoi(choice)
			if convErr != nil || idx < 1 || idx > len(msgs) {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid selection."); err != nil {
					return err
				}
				continue
			}
			if err := s.readMessage(term, &msgs[idx-1]); err != nil {
				return err
			}
		}
	}
}

func (s *Server) readMessage(term *Terminal, m *message.Message) error {
	rule := ansi.FG(ansi.Cyan, true) + strings.Repeat("-", 40) + ansi.Reset
	lines := []string{
		"",
		rule,
		fmt.Sprintf("From:    %s", m.FromName),
		fmt.Sprintf("To:      %s", m.ToName),
		fmt.Sprintf("Subject: %s", m.Subject),
		fmt.Sprintf("Date:    %s", m.PostedAt.Format("2006-01-02 15:04")),
		rule,
		m.Body,
		rule,
	}
	for _, line := range lines {
		if err := term.Println(line); err != nil {
			return err
		}
	}
	return nil
}

// postMessage prompts for a subject and a multi-line body (terminated
// by a lone "." on its own line, the classic BBS message-editor
// convention) and stores the result.
func (s *Server) postMessage(term *Terminal, u *user.User, area *message.Area) error {
	if err := term.Print(ansi.Reset + "\nSubject: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	subject, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	if err := term.Println(ansi.Reset + fmt.Sprintf("Enter your message. End with a single %q on its own line.", postBodyTerminator)); err != nil {
		return err
	}
	var bodyLines []string
	for {
		line, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		if line == postBodyTerminator {
			break
		}
		bodyLines = append(bodyLines, line)
	}

	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", subject, strings.Join(bodyLines, "\n")); err != nil {
		return err
	}
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Message posted.")
}

// sysopCreateArea is the "builtin:createarea" command: it prompts for
// a new area's tag, name, description, and SL gates.
func (s *Server) sysopCreateArea(term *Terminal, sysop *user.User) error {
	if err := term.Print(ansi.Reset + "\nArea tag (short, no spaces): " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	tag, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	if err := term.Print(ansi.Reset + "Area name: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	name, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	if err := term.Print(ansi.Reset + "Description: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	description, err := term.ReadLine(false)
	if err != nil {
		return err
	}

	minRead, err := s.promptSecurityLevel(term, "Minimum SL to read (0-255): ")
	if err != nil {
		return err
	}
	if minRead < 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid security level.")
	}

	minWrite, err := s.promptSecurityLevel(term, "Minimum SL to post (0-255): ")
	if err != nil {
		return err
	}
	if minWrite < 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid security level.")
	}

	area, err := s.Messages.CreateArea(tag, name, description, minRead, minWrite)
	if err != nil {
		if errors.Is(err, message.ErrTagTaken) {
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "That tag is already in use.")
		}
		return err
	}
	s.logInfo("%s created message area %q (%s)", sysop.Username, area.Name, area.Tag)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("Area %q created.", area.Name))
}

// promptSecurityLevel reads a 0-255 security level, returning -1 for
// any invalid or out-of-range input rather than an error, since that's
// treated as sysop input to reject, not a session-ending failure.
func (s *Server) promptSecurityLevel(term *Terminal, prompt string) (int, error) {
	if err := term.Print(ansi.Reset + prompt + ansi.FG(ansi.Yellow, true)); err != nil {
		return -1, err
	}
	input, err := term.ReadLine(false)
	if err != nil {
		return -1, err
	}
	level, convErr := strconv.Atoi(strings.TrimSpace(input))
	if convErr != nil || level < 0 || level > 255 {
		return -1, nil
	}
	return level, nil
}
