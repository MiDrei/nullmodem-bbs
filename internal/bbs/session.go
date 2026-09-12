package bbs

import (
	"errors"
	"fmt"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// Version is the BBS software version shown on the welcome screen and
// the [V]ersion menu command.
const Version = "NullModem BBS v0.1.0-dev"

// maxLoginAttempts is how many wrong passwords a session may try
// before being disconnected.
const maxLoginAttempts = 3

// minPasswordLength is the minimum length accepted at registration.
const minPasswordLength = 6

// Server drives BBS sessions handed to it by any transport (telnet,
// SSH, ...) that implements Conn.
type Server struct {
	Nodes     *NodeManager
	Users     *user.Store
	SysopName string
	BBSName   string
	NewUserSL int
}

// NewServer returns a Server ready to accept sessions.
func NewServer(bbsName, sysopName string, users *user.Store, newUserSL int) *Server {
	return &Server{
		Nodes:     NewNodeManager(),
		Users:     users,
		BBSName:   bbsName,
		SysopName: sysopName,
		NewUserSL: newUserSL,
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

	u, err := s.login(term)
	if err != nil {
		return
	}
	s.Nodes.SetUsername(node, u.Username)

	s.mainMenu(term, node, u)
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

// login prompts for a username, then either authenticates an existing
// account or walks a first-time caller through registration.
func (s *Server) login(term *Terminal) (*user.User, error) {
	for {
		if err := term.Print(ansi.Reset + "\nEnter your handle: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, err
		}
		handle, err := term.ReadLine(false)
		if err != nil {
			return nil, err
		}
		handle = strings.TrimSpace(handle)
		if handle == "" {
			continue
		}

		exists, err := s.Users.Exists(handle)
		if err != nil {
			return nil, err
		}
		if exists {
			u, err := s.authenticateExisting(term, handle)
			if err != nil {
				return nil, err
			}
			if u != nil {
				return u, nil
			}
			// Too many failed attempts; disconnect the session.
			return nil, fmt.Errorf("bbs: too many failed login attempts for %s", handle)
		}

		u, ok, err := s.registerNew(term, handle)
		if err != nil {
			return nil, err
		}
		if ok {
			return u, nil
		}
		// Registration declined; let them try a different handle.
	}
}

// authenticateExisting prompts for a password up to maxLoginAttempts
// times. It returns (nil, nil) if all attempts are exhausted, which
// the caller treats as a hard disconnect.
func (s *Server) authenticateExisting(term *Terminal, handle string) (*user.User, error) {
	for attempt := 1; attempt <= maxLoginAttempts; attempt++ {
		if err := term.Print(ansi.Reset + "Password: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, err
		}
		password, err := term.ReadLine(true)
		if err != nil {
			return nil, err
		}

		u, err := s.Users.Authenticate(handle, password)
		if err == nil {
			return u, nil
		}
		if !errors.Is(err, user.ErrInvalidCredentials) {
			return nil, err
		}
		if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid password."); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// registerNew offers to create a new account for a handle that does
// not exist yet. It returns ok=false if the caller declines, so login
// can loop back to the handle prompt.
func (s *Server) registerNew(term *Terminal, handle string) (*user.User, bool, error) {
	if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Green, true) + handle + " is a new handle."); err != nil {
		return nil, false, err
	}
	if err := term.Print("Create a new account? (Y/n) " + ansi.FG(ansi.Yellow, true)); err != nil {
		return nil, false, err
	}
	answer, err := term.ReadLine(false)
	if err != nil {
		return nil, false, err
	}
	if a := strings.ToUpper(strings.TrimSpace(answer)); a != "" && a != "Y" {
		return nil, false, nil
	}

	for {
		if err := term.Print(ansi.Reset + fmt.Sprintf("Choose a password (min %d chars): ", minPasswordLength) + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, false, err
		}
		pw1, err := term.ReadLine(true)
		if err != nil {
			return nil, false, err
		}
		if len(pw1) < minPasswordLength {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Password too short."); err != nil {
				return nil, false, err
			}
			continue
		}

		if err := term.Print(ansi.Reset + "Confirm password: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, false, err
		}
		pw2, err := term.ReadLine(true)
		if err != nil {
			return nil, false, err
		}
		if pw1 != pw2 {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Passwords did not match."); err != nil {
				return nil, false, err
			}
			continue
		}

		u, err := s.Users.Register(handle, pw1, s.NewUserSL)
		if err != nil {
			return nil, false, err
		}
		if err := term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Account created. Welcome, " + handle + "!"); err != nil {
			return nil, false, err
		}
		return u, true, nil
	}
}

func (s *Server) mainMenu(term *Terminal, node int, u *user.User) {
	for {
		menu := fmt.Sprintf(
			ansi.Reset+"\n"+
				ansi.FG(ansi.Green, true)+"Main Menu"+ansi.Reset+"\n"+
				"  [%sW%s]ho's online\n"+
				"  [%sY%s]our stats\n"+
				"  [%sV%s]ersion\n"+
				"  [%sQ%s]uit\n"+
				"\n"+ansi.FG(ansi.White, true)+"%s> "+ansi.Reset,
			ansi.FG(ansi.Yellow, true), ansi.FG(ansi.Green, true),
			ansi.FG(ansi.Yellow, true), ansi.FG(ansi.Green, true),
			ansi.FG(ansi.Yellow, true), ansi.FG(ansi.Green, true),
			ansi.FG(ansi.Yellow, true), ansi.FG(ansi.Green, true),
			u.Username,
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
		case "Y":
			s.showStats(term, u)
		case "V":
			term.Println("\n" + Version)
		case "Q":
			term.Println("\nGoodbye, " + u.Username + "!")
			return
		case "":
			// ignore blank input
		default:
			term.Println("\nUnknown command.")
		}
	}
}

func (s *Server) showStats(term *Terminal, u *user.User) {
	term.Println("\n" + ansi.FG(ansi.Cyan, true) + "Your account" + ansi.Reset)
	term.Println(fmt.Sprintf("Handle:         %s", u.Username))
	term.Println(fmt.Sprintf("Security level: %d", u.SecurityLevel))
	term.Println(fmt.Sprintf("Total calls:    %d", u.TotalCalls))
	term.Println(fmt.Sprintf("Member since:   %s", u.CreatedAt.Format("2006-01-02")))
}

func (s *Server) showWho(term *Terminal) {
	term.Println("\n" + ansi.FG(ansi.Cyan, true) + "Node  Handle               Terminal    Connected" + ansi.Reset)
	for _, n := range s.Nodes.Snapshot() {
		term.Println(fmt.Sprintf("%-6d%-21s%-12s%s", n.Node, n.Username, n.TermType, n.Connected.Format("15:04:05")))
	}
}
