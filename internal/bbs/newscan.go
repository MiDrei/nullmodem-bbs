package bbs

import (
	"fmt"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// The new scan: what's new since the last call, read in one go instead
// of area by area -- what every classic board offers after login.
//
//   - newScan (menu R) reads the unread messages of every area with
//     any, area after area, in the caller's area selection (the one
//     QWK packets use too; none chosen means every area).
//   - toMe (menu T) reads the unread messages addressed to the
//     caller's handle or real name, across all areas.
//   - loginSummary counts both and the unread netmail after login,
//     and offers to start reading.
//
// Read is read everywhere: the same message_reads as the area reader,
// the web portal and the mobile reader.

// scanExit is how the scan reader was left.
type scanExit int

const (
	scanDone     scanExit = iota // read to the end
	scanSkipArea                 // S: on to the next area, this one stays unread
	scanMarkArea                 // M: the rest of this area marked read
	scanStop                     // Q: stop scanning
)

// scanRead shows msgs one after another, marking each read as it's
// shown; areaOf is each message's area. withAreaKeys offers S and M.
func (s *Server) scanRead(term *Terminal, u *user.User, msgs []message.Message, start int, areaOf func(*message.Message) *message.Area, withAreaKeys bool) (scanExit, error) {
	hint := "[N] Next  [P] Prev  [Up/Dn] Scroll  [R] Reply  [Q] Stop"
	if withAreaKeys {
		hint = "[N] Next  [P] Prev  [R] Reply  [S] Skip area  [M] Area read  [Q] Stop"
	}
	idx, scrollOffset := start, 0
	for {
		m := &msgs[idx]
		area := areaOf(m)
		if err := s.Messages.MarkMessageRead(u.ID, m.ID); err != nil {
			return scanStop, err
		}
		maxOffset, err := s.drawReader(term, area, msgs, idx, scrollOffset, hint)
		if err != nil {
			return scanStop, err
		}
		key, err := term.ReadKey()
		if err != nil {
			return scanStop, err
		}
		switch {
		case key.Type == KeyUp:
			if scrollOffset > 0 {
				scrollOffset--
			}
		case key.Type == KeyDown:
			if scrollOffset < maxOffset {
				scrollOffset++
			}
		case key.Type == KeyLeft, isKey(key, 'p'):
			if idx > 0 {
				idx--
				scrollOffset = 0
			}
		case key.Type == KeyRight || key.Type == KeyEnter, isKey(key, 'n'):
			if idx == len(msgs)-1 {
				return scanDone, nil
			}
			idx++
			scrollOffset = 0
		case isKey(key, 'r'):
			if err := s.replyToMessage(term, u, area, m); err != nil {
				return scanStop, err
			}
		case withAreaKeys && isKey(key, 's'):
			return scanSkipArea, nil
		case withAreaKeys && isKey(key, 'm'):
			return scanMarkArea, nil
		case key.Type == KeyEscape, isKey(key, 'q'):
			return scanStop, nil
		}
	}
}

func isKey(k Key, r rune) bool {
	return k.Type == KeyChar && (k.Rune == r || k.Rune == r-'a'+'A')
}

// scanAreas are the caller's areas ("my areas", Telnet K) with
// something unread, in the area list's order.
func (s *Server) scanAreas(u *user.User) ([]message.AreaWithStats, error) {
	stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		return nil, err
	}
	mine, err := s.Messages.InMyAreas(u.ID)
	if err != nil {
		return nil, err
	}
	var out []message.AreaWithStats
	for _, st := range stats {
		if st.New > 0 && mine(st.Area.ID) {
			out = append(out, st)
		}
	}
	return out, nil
}

// newScan reads every unread message in the caller's areas.
func (s *Server) newScan(term *Terminal, u *user.User) error {
	areas, err := s.scanAreas(u)
	if err != nil {
		return err
	}
	if len(areas) == 0 {
		return s.scanNote(term, "No new messages.")
	}
	for _, st := range areas {
		area := st.Area
		msgs, err := s.Messages.UnreadMessages(area.ID, u.ID)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			continue
		}
		exit, err := s.scanRead(term, u, msgs, 0, func(*message.Message) *message.Area { return &area }, true)
		if err != nil {
			return err
		}
		switch exit {
		case scanMarkArea:
			if _, err := s.Messages.MarkAreaRead(u.ID, area.ID); err != nil {
				return err
			}
		case scanStop:
			return nil
		}
	}
	return s.scanNote(term, "End of the new messages.")
}

// toMeNames are the names a message to u may be addressed to.
func toMeNames(u *user.User) []string {
	return []string{u.Username, u.RealName}
}

// toMe reads the unread messages addressed to the caller.
func (s *Server) toMe(term *Terminal, u *user.User) error {
	msgs, err := s.Messages.UnreadToUser(u.ID, u.SecurityLevel, toMeNames(u))
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return s.scanNote(term, "No new messages to you.")
	}
	if _, err := s.scanRead(term, u, msgs, 0, s.areaLookup(), false); err != nil {
		return err
	}
	return nil
}

func (s *Server) scanNote(term *Terminal, text string) error {
	if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + text + ansi.Reset); err != nil {
		return err
	}
	return s.pauseForKey(term)
}

// loginSummary shows what's new since the last call and offers to read
// it; nothing when nothing is.
func (s *Server) loginSummary(term *Terminal, u *user.User) error {
	if s.Messages == nil || s.Netmail == nil {
		return nil
	}
	netmail, err := s.Netmail.UnreadCount(u.ID)
	if err != nil {
		s.logWarn("new mail summary: %v", err)
		return nil
	}
	toMe, err := s.Messages.UnreadToUser(u.ID, u.SecurityLevel, toMeNames(u))
	if err != nil {
		s.logWarn("new mail summary: %v", err)
		return nil
	}
	areas, err := s.scanAreas(u)
	if err != nil {
		s.logWarn("new mail summary: %v", err)
		return nil
	}
	newTotal := 0
	for _, a := range areas {
		newTotal += a.New
	}
	newFiles := 0
	if s.Files != nil {
		if _, n, err := s.Files.UnreadFiles(u.ID, u.SecurityLevel, 0); err == nil {
			newFiles = n
		}
	}
	if netmail == 0 && len(toMe) == 0 && newTotal == 0 && newFiles == 0 {
		return nil
	}

	label := func(text string) string { return ansi.FG(ansi.White, false) + fmt.Sprintf("  %-14s", text) }
	value := func(n int, what string) string {
		color := ansi.FG(ansi.White, false)
		if n > 0 {
			color = ansi.FG(ansi.Yellow, true)
		}
		return color + what + ansi.Reset + "\r\n"
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + "  New since your last call" + ansi.Reset + "\r\n")
	b.WriteString(ansi.FG(ansi.Blue, false) + "  " + strings.Repeat("\xc4", 40) + ansi.Reset + "\r\n")
	b.WriteString(label("Netmail") + value(netmail, plural(netmail, "unread netmail", "unread netmails")))
	b.WriteString(label("To you") + value(len(toMe), plural(len(toMe), "message", "messages")))
	b.WriteString(label("New") + value(newTotal, fmt.Sprintf("%s in %s",
		plural(newTotal, "message", "messages"), plural(len(areas), "area", "areas"))))
	b.WriteString(label("Files") + value(newFiles, plural(newFiles, "new file", "new files")))

	var keys []string
	if newTotal > 0 {
		keys = append(keys, "[R] Read new")
	}
	if len(toMe) > 0 {
		keys = append(keys, "[T] To you")
	}
	if netmail > 0 {
		keys = append(keys, "[N] Netmail")
	}
	if newFiles > 0 {
		keys = append(keys, "[F] Files")
	}
	keys = append(keys, "[Enter] Main menu")
	b.WriteString("\r\n  " + ansi.FG(ansi.Yellow, true) + strings.Join(keys, "  ") + ansi.Reset + " ")
	if err := term.Print(b.String()); err != nil {
		return err
	}
	for {
		key, err := term.ReadKey()
		if err != nil {
			return err
		}
		switch {
		case newTotal > 0 && isKey(key, 'r'):
			return s.newScan(term, u)
		case len(toMe) > 0 && isKey(key, 't'):
			return s.toMe(term, u)
		case netmail > 0 && isKey(key, 'n'):
			return s.showNetmail(term, u)
		case newFiles > 0 && isKey(key, 'f'):
			return s.newFiles(term, u)
		case key.Type == KeyEnter, key.Type == KeyEscape, isKey(key, 'q'):
			return nil
		}
	}
}

// plural is "1 message" or "3 messages".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// areaLookup returns each message's area, looked up once.
func (s *Server) areaLookup() func(*message.Message) *message.Area {
	areas := map[int64]*message.Area{}
	return func(m *message.Message) *message.Area {
		if a, ok := areas[m.AreaID]; ok {
			return a
		}
		a, err := s.Messages.AreaByID(m.AreaID)
		if err != nil {
			a = &message.Area{ID: m.AreaID, Name: "?"}
		}
		areas[m.AreaID] = a
		return a
	}
}

// searchMessages asks for words and lists the messages that have them
// (subject, text, from, to) in the areas the caller may read; a number
// reads from there, N/P stepping through the results.
func (s *Server) searchMessages(term *Terminal, u *user.User) error {
	if err := term.Print(ansi.Reset + "\r\nSearch messages (subject, text, from, to; Enter = back): " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	q, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	// Stored text is CP437, as typed here: searched as it is.
	if q = strings.TrimSpace(q); q == "" {
		return term.Print(ansi.Reset)
	}
	msgs, err := s.Messages.Search(u.SecurityLevel, q, 0, 100)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return s.scanNote(term, "Nothing found.")
	}
	areaOf := s.areaLookup()
	perPage := max(5, term.Height()-6)
	start := 0
	for {
		end := min(start+perPage, len(msgs))
		var b strings.Builder
		b.WriteString(ansi.ClearScreen() + ansi.Reset + ansi.FG(ansi.Cyan, true) + "  Messages matching \"" + q + "\"" + ansi.Reset + "\r\n\r\n")
		cut := func(v string, n int) string {
			r := []rune(v)
			if len(r) > n {
				r = r[:n]
			}
			return string(r) + strings.Repeat(" ", n-len(r))
		}
		for i := start; i < end; i++ {
			m := &msgs[i]
			fmt.Fprintf(&b, "  %s%4d%s  %s%s%s %s %s%s %s%s\r\n",
				ansi.FG(ansi.Yellow, true), i+1, ansi.Reset,
				ansi.FG(ansi.White, true), cut(m.Subject, 28), ansi.Reset,
				cut(m.FromName, 15),
				ansi.FG(ansi.Cyan, false), cut(areaOf(m).Tag, 14),
				ansi.FG(ansi.White, false)+term.Time(m.PostedAt).Format("2006-01-02"), ansi.Reset)
		}
		keys := "number = read (N/P to step on)"
		if end < len(msgs) {
			keys += ", Enter = more"
		}
		fmt.Fprintf(&b, "\r\n  %s, Q = back (%d-%d of %d): %s", keys, start+1, end, len(msgs), ansi.FG(ansi.Yellow, true))
		if err := term.Print(b.String()); err != nil {
			return err
		}
		in, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		in = strings.ToUpper(strings.TrimSpace(in))
		switch {
		case in == "Q", in == "" && end >= len(msgs):
			return term.Print(ansi.Reset)
		case in == "":
			start = end
		default:
			if n, err := strconv.Atoi(in); err == nil && n >= 1 && n <= len(msgs) {
				if _, err := s.scanRead(term, u, msgs, n-1, areaOf, false); err != nil {
					return err
				}
			}
		}
	}
}
