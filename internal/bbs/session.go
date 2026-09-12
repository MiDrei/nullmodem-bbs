package bbs

import (
	"fmt"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
)

// Version is the BBS software version shown on the welcome screen and
// the [V]ersion menu command.
const Version = "NullModem BBS v0.1.0-dev"

// Server drives BBS sessions handed to it by any transport (telnet,
// SSH, ...) that implements Conn.
type Server struct {
	Nodes     *NodeManager
	SysopName string
	BBSName   string
}

// NewServer returns a Server ready to accept sessions.
func NewServer(bbsName, sysopName string) *Server {
	return &Server{
		Nodes:     NewNodeManager(),
		BBSName:   bbsName,
		SysopName: sysopName,
	}
}

// Handle drives one client connection through login and the main menu
// until the client disconnects or quits. It registers/deregisters the
// session with the node manager and never lets a panic in menu logic
// take down the listener goroutine.
func (s *Server) Handle(conn Conn) {
	node := s.Nodes.Join(conn.RemoteAddr().String(), conn.TermType())
	defer s.Nodes.Leave(node)

	term := NewTerminal(conn)
	defer func() { recover() }()

	if err := s.welcome(term); err != nil {
		return
	}

	handle, err := s.login(term)
	if err != nil {
		return
	}
	s.Nodes.SetUsername(node, handle)

	s.mainMenu(term, node, handle)
}

func (s *Server) welcome(term *Terminal) error {
	banner := ansi.ClearScreen() +
		ansi.FG(ansi.Cyan, true) + strings.Repeat("=", 60) + "\n" +
		ansi.FG(ansi.White, true) + centered(s.BBSName, 60) + "\n" +
		ansi.FG(ansi.Cyan, false) + centered(Version, 60) + "\n" +
		ansi.FG(ansi.Cyan, true) + strings.Repeat("=", 60) + "\n" +
		ansi.Reset
	return term.Print(banner)
}

func centered(s string, width int) string {
	if len(s) >= width {
		return s
	}
	pad := (width - len(s)) / 2
	return strings.Repeat(" ", pad) + s
}

// login prompts for a handle. There is no persistent user database
// yet (Phase 1 roadmap item), so any non-empty handle is accepted.
func (s *Server) login(term *Terminal) (string, error) {
	for {
		if err := term.Print(ansi.Reset + "\nEnter your handle: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return "", err
		}
		handle, err := term.ReadLine(false)
		if err != nil {
			return "", err
		}
		handle = strings.TrimSpace(handle)
		if handle != "" {
			return handle, nil
		}
	}
}

func (s *Server) mainMenu(term *Terminal, node int, handle string) {
	for {
		menu := fmt.Sprintf(
			ansi.Reset+"\n"+
				ansi.FG(ansi.Green, true)+"Main Menu"+ansi.Reset+"\n"+
				"  [%sW%s]ho's online\n"+
				"  [%sV%s]ersion\n"+
				"  [%sQ%s]uit\n"+
				"\n"+ansi.FG(ansi.White, true)+"%s> "+ansi.Reset,
			ansi.FG(ansi.Yellow, true), ansi.FG(ansi.Green, true),
			ansi.FG(ansi.Yellow, true), ansi.FG(ansi.Green, true),
			ansi.FG(ansi.Yellow, true), ansi.FG(ansi.Green, true),
			handle,
		)
		if err := term.Print(menu); err != nil {
			return
		}

		choice, err := term.ReadLine(false)
		if err != nil {
			return
		}

		switch strings.ToUpper(strings.TrimSpace(choice)) {
		case "W":
			s.showWho(term)
		case "V":
			term.Println("\n" + Version)
		case "Q":
			term.Println("\nGoodbye, " + handle + "!")
			return
		case "":
			// ignore blank input
		default:
			term.Println("\nUnknown command.")
		}
	}
}

func (s *Server) showWho(term *Terminal) {
	term.Println("\n" + ansi.FG(ansi.Cyan, true) + "Node  Handle               Terminal    Connected" + ansi.Reset)
	for _, n := range s.Nodes.Snapshot() {
		term.Println(fmt.Sprintf("%-6d%-21s%-12s%s", n.Node, n.Username, n.TermType, n.Connected.Format("15:04:05")))
	}
}
