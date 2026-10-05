package bbs

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// Callers meeting each other: the one-liners wall, node messages
// between callers online at the same time, the teleconference, and
// paging the sysop -- who answers in the web admin, or on a node.
//
// Rooms and one-liners are in the database (internal/chat), shared
// with the web admin; they're stored as UTF-8 and turned into CP437
// for the terminal (and back for what a caller types).

// fromCP437 is what a caller typed, as UTF-8: CP437 bytes -- unless
// they're already UTF-8 (a modern terminal sends that: an emoji typed
// there must not end up as four CP437 characters). A CP437 byte above
// 0x7F alone is never valid UTF-8, and real CP437 text forming valid
// UTF-8 sequences is next to impossible in what people type.
func fromCP437(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return ansi.DecodeCP437([]byte(s))
}
func toCP437(s string) string { return string(ansi.EncodeCP437(s)) }

// nodeMessages are messages waiting for a node's next prompt.
type nodeMessages struct {
	mu  sync.Mutex
	box map[int][]string
}

func (n *nodeMessages) send(node int, text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.box == nil {
		n.box = map[int][]string{}
	}
	n.box[node] = append(n.box[node], text)
}

func (n *nodeMessages) take(node int) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	m := n.box[node]
	delete(n.box, node)
	return m
}

// showNodeMessages prints what's waiting for this node.
func (s *Server) showNodeMessages(term *Terminal) error {
	msgs := s.nodeMsgs.take(term.Node)
	if len(msgs) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	for _, m := range msgs {
		b.WriteString(ansi.FG(ansi.Magenta, true) + "\a" + toCP437(m) + ansi.Reset + "\r\n")
	}
	return term.Print(b.String())
}

// notifyNodes leaves text for every node (but except) where pick is true.
func (s *Server) notifyNodes(except int, pick func(username string) bool, text string) {
	nodes, err := s.Nodes.List()
	if err != nil {
		return
	}
	for _, n := range nodes {
		if n.Node != except && n.Username != "" && pick(n.Username) {
			s.nodeMsgs.send(n.Node, text)
		}
	}
}

// sendNodeMessage is W's follow-up: a line to another caller online.
func (s *Server) sendNodeMessage(term *Terminal, u *user.User) error {
	nodes, err := s.Nodes.List()
	if err != nil {
		return err
	}
	var others []int
	for _, n := range nodes {
		if n.Node != term.Node && n.Username != "" {
			others = append(others, n.Node)
		}
	}
	if len(others) == 0 {
		return nil
	}
	if err := term.Print(ansi.Reset + "\r\n" + term.T("who.send_prompt") + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	in, err := term.ReadLine(false)
	if err != nil || strings.TrimSpace(in) == "" {
		return err
	}
	node, convErr := strconv.Atoi(strings.TrimSpace(in))
	found := false
	for _, n := range others {
		found = found || n == node
	}
	if convErr != nil || !found {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("who.nobody"))
	}
	if err := term.Print(ansi.Reset + term.T("who.message") + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	text, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	if text = strings.TrimSpace(fromCP437(text)); text == "" {
		return nil
	}
	s.nodeMsgs.send(node, i18n.T(s.boardLang(), "who.message_from", "USERNAME", u.Username, "NODE", term.Node, "TEXT", text))
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("who.sent", "NODE", node))
}

// ---- One-liners ----

const onelinersShown = 10

// showOneliners shows the wall and offers to add a line.
func (s *Server) showOneliners(term *Terminal, u *user.User) error {
	if s.Chat == nil {
		return nil
	}
	list, err := s.Chat.Oneliners(onelinersShown)
	if err != nil {
		s.logWarn("one-liners: %v", err)
		return nil
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + "  " + term.T("oneliners.title") + ansi.Reset + "\r\n")
	b.WriteString(ansi.FG(ansi.Blue, false) + "  " + strings.Repeat("\xc4", 76) + ansi.Reset + "\r\n")
	if len(list) == 0 {
		b.WriteString(ansi.FG(ansi.White, false) + "  " + term.T("oneliners.empty") + ansi.Reset + "\r\n")
	}
	for _, o := range list {
		name := o.Username
		if len(name) > 14 {
			name = name[:14]
		}
		fmt.Fprintf(&b, "  %s%-14s %s%s%s\r\n", ansi.FG(ansi.Yellow, true), toCP437(name), ansi.FG(ansi.White, false), toCP437(o.Text), ansi.Reset)
	}
	if err := term.Print(b.String()); err != nil {
		return err
	}
	if !u.Validated {
		return s.pauseForKey(term)
	}
	if err := term.Print("\r\n  " + term.T("oneliners.add") + " " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	answer, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	if a := strings.ToUpper(strings.TrimSpace(answer)); !isYes(term, a) && a != "YES" {
		return term.Print(ansi.Reset)
	}
	if err := term.Print(ansi.Reset + "  " + term.T("oneliners.your_line", "MAX", chat.MaxOneliner) + "\r\n  " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	text, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	if text = strings.TrimSpace(fromCP437(text)); text == "" {
		return term.Print(ansi.Reset)
	}
	if _, err := s.Chat.AddOneliner(u.ID, u.Username, text); err != nil {
		return err
	}
	s.logInfo("%s wrote a one-liner", u.Username)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "  " + term.T("oneliners.done") + ansi.Reset)
}

// ---- Teleconference and paging ----

// teleconference is the main room -- for a sysop with callers waiting,
// first the offer to join one of them.
func (s *Server) teleconference(term *Terminal, u *user.User) error {
	if s.Chat == nil {
		return nil
	}
	if u.SecurityLevel >= user.SLSysop {
		rooms, _ := s.Chat.Rooms()
		for _, r := range rooms {
			if !r.Paging {
				continue
			}
			who := strings.TrimPrefix(r.Name, chat.PagePrefix)
			if err := term.Print(ansi.Reset + "\r\n" + ansi.FG(ansi.Magenta, true) + term.T("chat.paging_you", "USERNAME", who) +
				ansi.Reset + " " + term.T("chat.join_them") + " " + ansi.FG(ansi.Yellow, true)); err != nil {
				return err
			}
			a, err := term.ReadLine(false)
			if err != nil {
				return err
			}
			if a = strings.ToUpper(strings.TrimSpace(a)); a == "" || isYes(term, a) || a == "YES" {
				_, err := s.chatRoom(term, u, r.Name, term.T("chat.with", "USERNAME", who), "", false)
				return err
			}
		}
	}
	// The rooms: /join switches, /q leaves.
	room := chat.Main
	for {
		title, intro := term.T("chat.title"), ""
		if info, err := s.Chat.RoomByName(room); err == nil {
			title = info.Title
			var bridges []string
			if info.DiscordChannel != "" {
				bridges = append(bridges, "Discord")
			}
			if info.MatrixRoom != "" {
				bridges = append(bridges, "Matrix")
			}
			if len(bridges) > 0 {
				title += " (+ " + strings.Join(bridges, ", ") + ")"
			}
			intro = info.Topic
		}
		next, err := s.chatRoom(term, u, room, title, intro, true)
		if err != nil || next == "" {
			return err
		}
		room = next
	}
}

// pageWait keeps a caller from paging more than once in a while.
var pageWait = 3 * time.Minute

// pageSysop asks why, tells the sysop (a push to their devices from
// the web daemon, a message on any node they're on) and waits in the
// caller's page room for them.
func (s *Server) pageSysop(term *Terminal, u *user.User) error {
	if s.Chat == nil {
		return nil
	}
	room := chat.PageRoom(u.Username)
	if last, err := s.Chat.Lines(room, 0, 50); err == nil {
		for i := len(last) - 1; i >= 0; i-- {
			if last[i].Kind == chat.Page && time.Since(last[i].At) < pageWait {
				_, err := s.chatRoom(term, u, room, term.T("chat.waiting"), term.U("chat.paged_recently"), false)
				return err
			}
		}
	}
	if err := term.Print(ansi.Reset + "\r\n" + term.T("chat.page_prompt") + "\r\n" + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	reason, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	if reason = strings.TrimSpace(fromCP437(reason)); reason == "" {
		return term.Print(ansi.Reset)
	}
	source := fmt.Sprintf("node %d", term.Node)
	if _, err := s.Chat.Post(room, u.Username, source, chat.Page, reason); err != nil {
		return err
	}
	s.logInfo("%s paged the sysop: %s", u.Username, reason)
	s.notifyNodes(term.Node, func(name string) bool {
		su, err := s.Users.ByUsername(name)
		return err == nil && su.SecurityLevel >= user.SLSysop
	}, i18n.T(s.boardLang(), "chat.page_note", "USERNAME", u.Username, "NODE", term.Node, "REASON", reason))
	_, err = s.chatRoom(term, u, room, term.T("chat.waiting"), term.U("chat.paged"), false)
	return err
}

// chatEvent is a key read for the chat screen.
type chatEvent struct {
	key Key
	err error
}

// chatRoom is the chat screen: what's said in room above, what the
// caller types below; Enter sends, /q (or Ctrl-Z) leaves. With rooms,
// /rooms lists the others and /join names the one to go to next
// (returned; "" when leaving).
func (s *Server) chatRoom(term *Terminal, u *user.User, room, title, intro string, rooms bool) (next string, err error) {
	source := fmt.Sprintf("node %d", term.Node)
	if err := s.Chat.Enter(room, u.Username, source); err != nil {
		return "", err
	}
	defer s.Chat.Exit(room, u.Username, source)

	width, height := term.Width(), term.Height()
	areaTop, areaRows := 3, height-5 // title, rule; then the lines; rule, input, hint
	if areaRows < 3 {
		areaRows = 3
	}
	var shown []string // rendered rows, newest last
	addRow := func(color, text string) {
		for _, r := range ansi.WrapText(toCP437(text), width) {
			shown = append(shown, color+r+ansi.Reset)
		}
		if len(shown) > 500 {
			shown = shown[len(shown)-500:]
		}
	}
	comings := false // someone joined or left: who's here changed
	addLine := func(l chat.Line) {
		if l.Kind == chat.Join || l.Kind == chat.Leave {
			comings = true
		}
		at := term.Time(l.At).Format("15:04")
		switch l.Kind {
		case chat.Join:
			addRow(ansi.FG(ansi.Green, false), at+"  "+term.U("chat.joined", "USERNAME", l.Username, "SOURCE", l.Source))
		case chat.Leave:
			addRow(ansi.FG(ansi.Green, false), at+"  "+term.U("chat.left", "USERNAME", l.Username))
		case chat.Page:
			addRow(ansi.FG(ansi.Magenta, true), at+"  "+term.U("chat.paged_line", "USERNAME", l.Username, "TEXT", l.Text))
		default:
			addRow(ansi.FG(ansi.White, false), fmt.Sprintf("%s  %s: %s", at, chat.Speaker(l), l.Text))
		}
	}
	if intro != "" {
		addRow(ansi.FG(ansi.Yellow, true), intro)
	}
	var lastID int64
	if recent, err := s.Chat.Lines(room, 0, areaRows); err == nil {
		for _, l := range recent {
			addLine(l)
			lastID = l.ID
		}
	}

	var input []byte
	present := ""
	headline := func() string {
		return fmt.Sprintf("\x1b[1;1H%s%s%s  %s%s\x1b[K", ansi.Reset, ansi.FG(ansi.Cyan, true), title, ansi.FG(ansi.White, false), toCP437(present)+ansi.Reset)
	}
	area := func() string {
		var o strings.Builder
		start := max(0, len(shown)-areaRows)
		for i := 0; i < areaRows; i++ {
			fmt.Fprintf(&o, "\x1b[%d;1H%s\x1b[K", areaTop+i, ansi.Reset)
			if start+i < len(shown) {
				o.WriteString(shown[start+i])
			}
		}
		return o.String()
	}
	inputRow := func() string {
		visible := input
		if len(visible) > width-3 {
			visible = visible[len(visible)-(width-3):]
		}
		return fmt.Sprintf("\x1b[%d;1H%s%s> %s%s\x1b[K", areaTop+areaRows+1, ansi.Reset, ansi.FG(ansi.Yellow, true), ansi.Reset, visible)
	}
	hint := term.T("chat.hint")
	if rooms {
		hint = term.T("chat.hint_rooms")
	}
	full := func() error {
		rule := ansi.FG(ansi.Blue, false) + strings.Repeat("\xc4", width) + ansi.Reset
		return term.Print(ansi.ClearScreen() + headline() +
			fmt.Sprintf("\x1b[2;1H%s", rule) + area() +
			fmt.Sprintf("\x1b[%d;1H%s", areaTop+areaRows, rule) +
			fmt.Sprintf("\x1b[%d;1H%s%s%s", areaTop+areaRows+2, ansi.FG(ansi.White, false), hint, ansi.Reset) +
			inputRow())
	}
	refreshPresent := func() {
		ps, err := s.Chat.Present(room)
		if err != nil {
			return
		}
		var names []string
		for _, p := range ps {
			n := p.Username
			if p.Source == "web" {
				n += " (web)"
			}
			names = append(names, n)
		}
		present = term.U("chat.here", "NAMES", strings.Join(names, ", "))
	}
	refreshPresent()
	if err := full(); err != nil {
		return "", err
	}

	// Keys come from a goroutine that reads one and waits to be told
	// to read the next -- so none is read (and lost) after leaving.
	events := make(chan chatEvent)
	nextKey := make(chan bool)
	go func() {
		for {
			k, err := term.ReadKey()
			events <- chatEvent{k, err}
			if err != nil || !<-nextKey {
				return
			}
		}
	}()
	poll := time.NewTicker(700 * time.Millisecond)
	defer poll.Stop()
	lastTouch := time.Now()

	for {
		select {
		case ev := <-events:
			if ev.err != nil {
				return "", ev.err
			}
			leave, redraw, joinRoom := false, false, ""
			switch k := ev.key; {
			case k.Type == KeyChar:
				if len(input) < chat.MaxText {
					input = append(input, byte(k.Rune))
				}
			case k.Type == KeyBackspace:
				if len(input) > 0 {
					input = input[:len(input)-1]
				}
			case k.Type == KeyCtrl && k.Rune == 'z':
				leave = true
			case k.Type == KeyCtrl && k.Rune == 'l':
				redraw = true
			case k.Type == KeyEnter:
				text := strings.TrimSpace(fromCP437(string(input)))
				input = input[:0]
				switch strings.ToLower(text) {
				case "":
				case "/q", "/quit", "/x":
					leave = true
				case "/who":
					refreshPresent()
					addRow(ansi.FG(ansi.Cyan, false), present)
					redraw = true
				case "/rooms", "/list":
					if rooms {
						s.listChatRooms(term, u, room, func(line string) { addRow(ansi.FG(ansi.Cyan, false), line) })
						redraw = true
						break
					}
					fallthrough
				default:
					if name, ok := strings.CutPrefix(strings.ToLower(text), "/join "); ok && rooms {
						target, msg := s.chatJoinTarget(term, u, room, strings.TrimSpace(name))
						if target != "" {
							joinRoom, leave = target, true
						} else {
							addRow(ansi.FG(ansi.Red, true), msg)
							redraw = true
						}
						break
					}
					if _, err := s.Chat.Post(room, u.Username, source, chat.Say, text); err != nil {
						s.logWarn("chat: %v", err)
					}
					s.pollChat(room, &lastID, addLine)
					redraw = true
				}
			}
			if leave {
				nextKey <- false
				term.Print(ansi.ClearScreen() + ansi.Reset)
				return joinRoom, nil
			}
			nextKey <- true
			if redraw {
				if err := term.Print(headline() + area() + inputRow()); err != nil {
					return "", err
				}
			} else if err := term.Print(inputRow()); err != nil {
				return "", err
			}
		case <-poll.C:
			changed := s.pollChat(room, &lastID, addLine)
			for _, m := range s.nodeMsgs.take(term.Node) {
				addRow(ansi.FG(ansi.Magenta, true), m)
				changed = true
			}
			if time.Since(lastTouch) > 5*time.Second {
				s.Chat.Touch(room, u.Username, source)
				lastTouch = time.Now()
				comings = true
			}
			if comings {
				refreshPresent()
				comings = false
				changed = true
			}
			if changed {
				if err := term.Print(headline() + area() + inputRow()); err != nil {
					return "", err
				}
			}
		}
	}
}

// listChatRooms adds a line per room u may enter, with who's there.
func (s *Server) listChatRooms(term *Terminal, u *user.User, current string, add func(string)) {
	list, err := s.Chat.RoomsFor(u.SecurityLevel)
	if err != nil {
		return
	}
	add(term.U("chat.rooms"))
	for _, r := range list {
		mark := "  "
		if r.Name == current {
			mark = "> "
		}
		line := fmt.Sprintf("%s%-12s %s", mark, r.Name, r.Title)
		if ps, err := s.Chat.Present(r.Name); err == nil && len(ps) > 0 {
			line += "  " + term.U("chat.n_here", "COUNT", len(ps))
		}
		if r.DiscordChannel != "" {
			line += "  + Discord"
		}
		if r.MatrixRoom != "" {
			line += "  + Matrix"
		}
		add(line)
	}
}

// chatJoinTarget checks a /join: the room to go to, or why not.
func (s *Server) chatJoinTarget(term *Terminal, u *user.User, current, name string) (string, string) {
	r, err := s.Chat.RoomByName(name)
	switch {
	case name == "":
		return "", term.U("chat.join_which")
	case err != nil || u.SecurityLevel < r.MinSL:
		return "", term.U("chat.no_room", "ROOM", name)
	case r.Name == current:
		return "", term.U("chat.in_it")
	}
	return r.Name, ""
}

// pollChat adds room's lines after *lastID; true if there were any.
func (s *Server) pollChat(room string, lastID *int64, add func(chat.Line)) bool {
	lines, err := s.Chat.Lines(room, *lastID, 100)
	if err != nil || len(lines) == 0 {
		return false
	}
	for _, l := range lines {
		add(l)
		*lastID = l.ID
	}
	return true
}
