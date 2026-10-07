package bbs

import (
	"net"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/doors"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/kit/ansi"
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
	if ok, err := s.mayPost(term, u); !ok {
		return err
	}
	all := s.Doors
	if s.LoadDoors != nil {
		all = s.LoadDoors()
	}
	var available []doors.Door
	for _, d := range all {
		if u.SecurityLevel >= d.MinSL {
			available = append(available, d)
		}
	}
	if len(available) == 0 {
		return term.Println(ansi.Reset + "\n" + term.T("doors.none"))
	}

	for {
		bulletins := doorBulletinList(available)
		if err := term.Print(s.renderDoorList(term, u, available, len(bulletins) > 0)); err != nil {
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
		if strings.EqualFold(choice, "b") && len(bulletins) > 0 {
			if err := s.showDoorBulletins(term, u, bulletins); err != nil {
				return err
			}
			continue
		}
		idx, convErr := strconv.Atoi(choice)
		if convErr != nil || idx < 1 || idx > len(available) {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("common.invalid_selection")); err != nil {
				return err
			}
			continue
		}
		started := time.Now()
		err = s.playDoor(term, u, available[idx-1])
		if serr := s.Stats.RecordDoor(available[idx-1].Name, u.ID, time.Since(started)); serr != nil {
			s.logWarn("%v", serr)
		}
		if err != nil {
			return err
		}
	}
}

// The door list's optional hand-designed pieces: a banner (clearing
// the screen, like renderAreaHeader's), one row per door and for the
// B and Q entries, a gap before those two, and a closing part -- each
// with a plain fallback, so the list looks as it always did without
// them.
const (
	doorsScreen       = "doors.ans"
	doorsRowScreen    = "doors-row.ans"
	doorsGapScreen    = "doors-gap.ans"
	doorsFooterScreen = "doors-footer.ans"

	fallbackDoorsRow = "{KEY:2}) {DOOR}"
)

// renderDoorList is the door list as printed before asking which to
// play: the banner, a row per door, the bulletins (if any door has
// some) and back entries, and the prompt.
func (s *Server) renderDoorList(term *Terminal, u *user.User, available []doors.Door, bulletins bool) string {
	var items [][2]string
	for i, d := range available {
		items = append(items, [2]string{strconv.Itoa(i + 1), d.Name})
	}
	var tail [][2]string
	if bulletins {
		tail = append(tail, [2]string{"B", term.T("doors.bulletins_item")})
	}
	tail = append(tail, [2]string{"Q", term.T("doors.back")})
	return s.renderDoorFrame(term, u, doorsScreen, term.T("common.doors"), items, tail, term.T("doors.which"))
}

// renderDoorFrame is a list in the door list's frame: banner (header,
// with title as the fallback), a row per item, the gap, the tail
// entries, the frame's bottom and the prompt.
func (s *Server) renderDoorFrame(term *Terminal, u *user.User, header, title string, items, tail [][2]string, prompt string) string {
	row := s.loadOptionalScreen(term, doorsRowScreen, fallbackDoorsRow)
	line := func(key, label string) string {
		return ansi.Layout(ansi.Render(row, ansi.Vars{"KEY": key, "DOOR": label}), term.Width()) + ansi.Reset + "\r\n"
	}
	var b strings.Builder
	b.WriteString(s.featureHeader(term, u, header, title))
	for _, it := range items {
		b.WriteString(line(it[0], it[1]))
	}
	if gap := s.loadOptionalScreen(term, doorsGapScreen, ""); gap != "" {
		b.WriteString(ansi.Layout(gap, term.Width()) + ansi.Reset + "\r\n")
	}
	for _, it := range tail {
		b.WriteString(line(it[0], it[1]))
	}
	if footer := s.loadOptionalScreen(term, doorsFooterScreen, ""); footer != "" {
		b.WriteString(ansi.Layout(footer, term.Width()) + ansi.Reset + "\r\n")
	}
	b.WriteString("\r\n" + fgDim(ansi.White) + prompt + " " + ansi.FG(ansi.Yellow, true))
	return b.String()
}

// playDoor hands the connection's raw byte stream to internal/doors
// for the duration of one play session (see doors.Run and
// Terminal.Raw) and reports back to the menu once the door exits --
// an abnormal exit is logged, not propagated, the same way
// downloadFile/uploadFile treat a failed Zmodem transfer as something
// to tell the caller about rather than a reason to drop their whole
// BBS session.
func (s *Server) playDoor(term *Terminal, u *user.User, door doors.Door) error {
	if door.Kind == "rlogin" {
		return s.playRemoteDoor(term, u, door)
	}
	if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) +
		term.T("doors.launching", "DOOR", door.Name) + ansi.Reset + "\r\n"); err != nil {
		return err
	}

	node := term.Node
	if node < 1 {
		node = 1
	}
	sess := doors.Session{
		// The handle, not u.RealName, on purpose (decided 2026-09-25):
		// doors key players and save games on this name, and a caller
		// changing their real name in the profile must not lose theirs.
		RealName:        u.Username,
		Handle:          u.Username,
		AccessLevel:     u.SecurityLevel,
		TimeLeftMinutes: 60,
		Node:            node,
		UserID:          u.ID,
		TotalCalls:      u.TotalCalls,
		BBSName:         s.BBSName,
		SysopName:       s.SysopName,
		RemoteIP:        remoteIP(term.Raw().RemoteAddr()),
	}
	if u.LastLoginAt.Valid {
		sess.LastCall = u.LastLoginAt.Time
	}
	sess.DropLineEnd = term.TakeLineEnd()
	done := term.Busy() // the door keeps its own time
	if err := doors.Run(term.Raw(), door, sess); err != nil {
		s.logWarn("door %s ended abnormally for %s: %v", door.Name, u.Username, err)
	}
	done()
	return term.Println(ansi.Reset + "\r\n" + ansi.FG(ansi.Green, true) + term.T("doors.returned", "DOOR", door.Name))
}

// remoteIP is addr's IP address, or "" if it has none.
func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
		return ip.String()
	}
	return ""
}
