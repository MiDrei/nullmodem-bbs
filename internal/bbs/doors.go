package bbs

import (
	"fmt"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/doors"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// showDoors is the "builtin:doors" command: a numbered list (like
// sysopImportFile's area picker) of every door the caller's security
// level allows, letting them pick one to play or Q to go back. A
// lightbar isn't worth it here the way it is for message/file areas
// (browseArea, browseFileArea) -- the list is expected to stay short
// (a handful of configured doors, not hundreds of messages), and it's
// re-shown after every play session anyway, so there's no scrolling
// concern to solve.
func (s *Server) showDoors(term *Terminal, u *user.User) error {
	var available []doors.Door
	for _, d := range s.Doors {
		if u.SecurityLevel >= d.MinSL {
			available = append(available, d)
		}
	}
	if len(available) == 0 {
		return term.Println(ansi.Reset + "\nNo doors available.")
	}

	for {
		var b strings.Builder
		b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + "Doors" + ansi.Reset + "\r\n")
		for i, d := range available {
			b.WriteString(fmt.Sprintf("%2d) %s\r\n", i+1, d.Name))
		}
		b.WriteString(" Q) Back to menu\r\n\r\nPlay which? " + ansi.FG(ansi.Yellow, true))
		if err := term.Print(b.String()); err != nil {
			return err
		}

		choice, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		choice = strings.TrimSpace(choice)
		if choice == "" {
			continue
		}
		if strings.EqualFold(choice, "q") {
			return nil
		}
		idx, convErr := strconv.Atoi(choice)
		if convErr != nil || idx < 1 || idx > len(available) {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid selection."); err != nil {
				return err
			}
			continue
		}
		if err := s.playDoor(term, u, available[idx-1]); err != nil {
			return err
		}
	}
}

// playDoor hands the connection's raw byte stream to internal/doors
// for the duration of one play session (see doors.Run and
// Terminal.Raw) and reports back to the menu once the door exits --
// an abnormal exit is logged, not propagated, the same way
// downloadFile/uploadFile treat a failed Zmodem transfer as something
// to tell the caller about rather than a reason to drop their whole
// BBS session.
func (s *Server) playDoor(term *Terminal, u *user.User, door doors.Door) error {
	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		fmt.Sprintf("Launching %s...", door.Name) + ansi.Reset + "\r\n"); err != nil {
		return err
	}

	sess := doors.Session{
		RealName:        u.Username,
		Handle:          u.Username,
		AccessLevel:     u.SecurityLevel,
		TimeLeftMinutes: 60,
		Node:            1,
	}
	if err := doors.Run(term.Raw(), door, sess); err != nil {
		s.logWarn("door %s ended abnormally for %s: %v", door.Name, u.Username, err)
	}
	return term.Println(ansi.Reset + "\r\n" + ansi.FG(ansi.Green, true) + fmt.Sprintf("Returned from %s.", door.Name))
}
