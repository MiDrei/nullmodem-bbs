package bbs

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
	"git.maik.ch/swissmaik/nullmodem/internal/applog"
	"git.maik.ch/swissmaik/nullmodem/internal/doors"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/menu"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/session"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
	"git.maik.ch/swissmaik/nullmodem/internal/version"
)

// Version is the BBS software version shown on the welcome screen and
// the [V]ersion menu command.
const Version = version.Version

// maxLoginAttempts is how many wrong passwords a session may try
// before being disconnected.
const maxLoginAttempts = 3

// minPasswordLength is the minimum length accepted at registration.
const minPasswordLength = 6

// Server drives BBS sessions handed to it by any transport (telnet,
// SSH, ...) that implements Conn.
type Server struct {
	Nodes         *session.Store
	Users         *user.Store
	Menus         menu.Set
	Messages      *message.Store
	Files         *file.Store
	Netmail       *netmail.Store
	Doors         []doors.Door
	Logger        *applog.Logger
	SysopName     string
	BBSName       string
	FTNAddress    string
	NewUserSL     int
	WelcomeScreen string
	ScreensDir    string
}

// Options bundles the dependencies and configuration NewServer needs.
// It exists mainly so adding a new setting (like WelcomeScreen) doesn't
// require touching every call site's positional argument list.
type Options struct {
	BBSName       string
	SysopName     string
	FTNAddress    string
	Users         *user.Store
	Menus         menu.Set
	Messages      *message.Store
	Files         *file.Store
	Netmail       *netmail.Store
	Doors         []doors.Door
	Nodes         *session.Store
	Logger        *applog.Logger
	NewUserSL     int
	WelcomeScreen string
	ScreensDir    string
}

// NewServer returns a Server ready to accept sessions.
func NewServer(opts Options) *Server {
	return &Server{
		Nodes:         opts.Nodes,
		Users:         opts.Users,
		Menus:         opts.Menus,
		Messages:      opts.Messages,
		Files:         opts.Files,
		Netmail:       opts.Netmail,
		Doors:         opts.Doors,
		Logger:        opts.Logger,
		BBSName:       opts.BBSName,
		SysopName:     opts.SysopName,
		FTNAddress:    opts.FTNAddress,
		NewUserSL:     opts.NewUserSL,
		WelcomeScreen: opts.WelcomeScreen,
		ScreensDir:    opts.ScreensDir,
	}
}

// logInfo/logWarn are nil-safe wrappers around Server.Logger, which is
// optional (e.g. in tests that don't care about activity logging).
func (s *Server) logInfo(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Info(format, args...)
	}
}

func (s *Server) logWarn(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Warn(format, args...)
	}
}

// Handle drives one client connection through login and the main menu
// until the client disconnects or quits. It registers/deregisters the
// session with the shared session store and never lets a panic in
// menu logic take down the listener goroutine.
func (s *Server) Handle(conn Conn) {
	node, err := s.Nodes.Join(conn.RemoteAddr().String(), conn.TermType())
	if err != nil {
		return
	}
	s.logInfo("node %d connected from %s (%s)", node, conn.RemoteAddr(), conn.TermType())
	defer func() {
		s.logInfo("node %d disconnected", node)
		s.Nodes.Leave(node)
	}()

	term := NewTerminal(conn)
	defer func() { recover() }()

	if err := s.welcome(term, node); err != nil {
		return
	}

	u, err := s.login(term)
	if err != nil {
		return
	}
	s.Nodes.SetUsername(node, u.Username)
	s.logInfo("node %d: %s logged in", node, u.Username)

	if err := s.runMenu(term, u, node, "main"); err != nil && !errors.Is(err, errLogoff) {
		s.logWarn("node %d (%s): menu error: %v", node, u.Username, err)
		term.Println("\n" + ansi.FG(ansi.Red, true) + "Menu error: " + err.Error())
	}

	// The telnet/SSH listener closes conn the instant Handle returns
	// (see internal/telnet.Server's accept loop). Closing immediately
	// after a final write can race the OS/network into dropping that
	// write before the client ever sees it -- most visible on a real
	// terminal client disconnecting right after the logoff screen, so
	// give it a moment to actually reach the other end first.
	time.Sleep(300 * time.Millisecond)
}

func (s *Server) welcome(term *Terminal, node int) error {
	rendered := ansi.Render(s.WelcomeScreen, s.baseVars(node))
	return term.Print(ansi.Layout(rendered, term.Width()))
}

// baseVars are the placeholders available before login, when there is
// no authenticated user yet to describe.
func (s *Server) baseVars(node int) ansi.Vars {
	now := time.Now()
	return ansi.Vars{
		"BBSNAME": s.BBSName,
		"SYSOP":   s.SysopName,
		"VERSION": Version,
		"NODE":    strconv.Itoa(node),
		"DATE":    now.Format("2006-01-02"),
		"TIME":    now.Format("15:04:05"),
	}
}

// userVars extends baseVars with placeholders describing the
// authenticated caller, for use once login has completed.
func (s *Server) userVars(u *user.User, node int) ansi.Vars {
	vars := s.baseVars(node)
	vars["USERNAME"] = u.Username
	vars["SL"] = strconv.Itoa(u.SecurityLevel)
	vars["TOTALCALLS"] = strconv.Itoa(u.TotalCalls)
	return vars
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
			s.logWarn("too many failed login attempts for %s", handle)
			return nil, fmt.Errorf("bbs: too many failed login attempts for %s", handle)
		}

		if user.IsRestrictedUsername(handle) {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "That handle is reserved."); err != nil {
				return nil, err
			}
			continue
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

		realName, err := s.promptRealName(term)
		if err != nil {
			return nil, false, err
		}

		u, err := s.Users.Register(handle, pw1, s.NewUserSL)
		if err != nil {
			return nil, false, err
		}
		if err := s.Users.SetRealName(u.ID, realName); err != nil {
			return nil, false, err
		}
		u.RealName = realName
		s.logInfo("new account registered: %s (SL %d)", u.Username, u.SecurityLevel)
		if err := term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + "Account created. Welcome, " + handle + "!"); err != nil {
			return nil, false, err
		}
		if u.SecurityLevel >= user.SLSysop {
			if err := term.Println(ansi.FG(ansi.Yellow, true) + "You are the first user and have been granted sysop access (SL 255)."); err != nil {
				return nil, false, err
			}
		}
		return u, true, nil
	}
}

// promptRealName asks for a real name, required (many FTN networks
// reject a purely handle-only participant) and rejected if it's a
// reserved system/staff role (see user.IsRestrictedRealName) -- loops
// until a valid one is given, mirroring the password-confirmation
// loop just above it.
func (s *Server) promptRealName(term *Terminal) (string, error) {
	for {
		if err := term.Print(ansi.Reset + "Real name: " + ansi.FG(ansi.Yellow, true)); err != nil {
			return "", err
		}
		realName, err := term.ReadLine(false)
		if err != nil {
			return "", err
		}
		realName = strings.TrimSpace(realName)
		if realName == "" {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Real name is required."); err != nil {
				return "", err
			}
			continue
		}
		if user.IsRestrictedRealName(realName) {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "That name is reserved."); err != nil {
				return "", err
			}
			continue
		}
		return realName, nil
	}
}

// errLogoff signals that a "logoff" item was chosen, at any menu
// nesting depth, and must unwind all the way out of runMenu's
// recursion rather than just popping back to the calling menu.
var errLogoff = errors.New("bbs: logoff")

// builtins maps a menu item's "builtin:<name>" action to the handler
// it runs. Adding a new builtin command means adding an entry here
// and referencing "builtin:<name>" from a menu YAML file.
var builtins = map[string]func(s *Server, term *Terminal, u *user.User) error{
	"who":            (*Server).showWho,
	"stats":          (*Server).showStats,
	"version":        (*Server).showVersion,
	"listusers":      (*Server).sysopListUsers,
	"setsl":          (*Server).sysopSetSecurityLevel,
	"areas":          (*Server).showAreas,
	"createarea":     (*Server).sysopCreateArea,
	"files":          (*Server).showFileAreas,
	"createfilearea": (*Server).sysopCreateFileArea,
	"importfile":     (*Server).sysopImportFile,
	"netmail":        (*Server).showNetmail,
	"doors":          (*Server).showDoors,
	"qwk":            (*Server).downloadQWK,
	"qwkrep":         (*Server).uploadQWKReply,
	"qwkareas":       (*Server).configureQWKAreas,
}

// runMenu displays the named menu and dispatches choices until the
// caller logs off, an unrecoverable I/O error occurs, or a "goto"
// leads into a menu that itself returns (at which point this menu
// resumes, so nested menus behave like a stack of screens).
func (s *Server) runMenu(term *Terminal, u *user.User, node int, name string) error {
	m, ok := s.Menus.Get(name)
	if !ok {
		return fmt.Errorf("menu %q not found", name)
	}

	for {
		rendered := s.renderMenuDisplay(m, u, node)
		if err := term.Print(ansi.Layout(rendered, term.Width())); err != nil {
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

		item, ok := m.Find(choice, u.SecurityLevel)
		if !ok {
			if err := term.Println("\nUnknown command."); err != nil {
				return err
			}
			continue
		}

		switch {
		case item.Action == "logoff":
			if err := s.printLogoffScreen(term, u, node); err != nil {
				return err
			}
			return errLogoff

		case strings.HasPrefix(item.Action, "goto:"):
			target := strings.TrimPrefix(item.Action, "goto:")
			if err := s.runMenu(term, u, node, target); err != nil {
				return err
			}

		case strings.HasPrefix(item.Action, "builtin:"):
			name := strings.TrimPrefix(item.Action, "builtin:")
			fn, ok := builtins[name]
			if !ok {
				if err := term.Println("\nUnimplemented command: " + name); err != nil {
					return err
				}
				continue
			}
			if err := fn(s, term, u); err != nil {
				return err
			}

		default:
			if err := term.Println("\nUnrecognized menu action: " + item.Action); err != nil {
				return err
			}
		}
	}
}

// renderMenuDisplay returns what to print for one pass through a
// menu loop: m.Screen verbatim (with placeholders filled in) if the
// menu has a hand-designed screen, falling back to the generated
// title+item-list text otherwise -- including when the screen file
// is missing or unreadable, so a typo'd filename degrades gracefully
// instead of locking callers out of the menu.
func (s *Server) renderMenuDisplay(m *menu.Menu, u *user.User, node int) string {
	vars := s.userVars(u, node)
	if m.Screen != "" {
		raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, m.Screen))
		if err != nil {
			s.logWarn("menu %q: could not load screen %q: %v", m.Name, m.Screen, err)
		} else {
			return ansi.Render(raw, vars)
		}
	}
	return renderMenu(m, u.SecurityLevel, vars)
}

// logoffScreenFile is the fixed, convention-based filename for the
// optional hand-designed goodbye screen, the same way cmd/bbs always
// loads "welcome.ans" for the connect banner -- there's no menu.Menu
// to hang a Screen field off of for a logoff, since it's a builtin
// action rather than a named menu.
const logoffScreenFile = "logoff.ans"

// printLogoffScreen shows logoff.ans (with placeholders filled in) if
// present, falling back to a plain goodbye line otherwise.
func (s *Server) printLogoffScreen(term *Terminal, u *user.User, node int) error {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, logoffScreenFile))
	if err != nil {
		return term.Println("\nGoodbye, " + u.Username + "!")
	}
	rendered := ansi.Render(raw, s.userVars(u, node))
	return term.Print(ansi.Layout(rendered, term.Width()))
}

// trailingEscapesPattern matches a run of ANSI/CSI escape sequences
// (no visible characters) anchored to the end of a string -- see
// finishHeaderLine, which needs to look past a trailing SGR reset to
// find whether a real line terminator already precedes it.
var trailingEscapesPattern = regexp.MustCompile(`(?:\x1b\[[0-9;]*[A-Za-z])*$`)

// finishHeaderLine ensures rendered ends with at least one line
// terminator, appending exactly one "\r\n" if it doesn't already --
// so a caller that immediately appends its own next section afterward
// starts on a fresh row, never mid-line. Critically, it never adds a
// SECOND terminator on top of one rendered already has: every real
// .ans screen file ends with its own "\r\n" for the last visible row
// followed by an invisible SGR reset code ("\x1b[0m") with nothing
// after it, so this looks PAST any such trailing escape run before
// deciding whether a terminator is already there, rather than just
// checking the string's literal last bytes (which are always the
// reset code, never "\r\n", so a naive check would add a redundant
// one every time). If the file already has one or more of its own
// trailing blank lines, those are left exactly as authored -- this
// only ever adds the single terminator needed for proper line
// termination, never a gap of its own that the sysop didn't ask for.
func finishHeaderLine(rendered string) string {
	loc := trailingEscapesPattern.FindStringIndex(rendered)
	core := rendered
	if loc != nil {
		core = rendered[:loc[0]]
	}
	if strings.HasSuffix(core, "\n") || strings.HasSuffix(core, "\r") {
		return rendered
	}
	return rendered + "\r\n"
}

// renderAreaHeader returns a hand-designed banner screen (with
// placeholders filled in, and expected to clear the screen itself the
// way every other hand-designed screen does) for display above a
// message/file area listing when screenFile exists in ScreensDir,
// falling back to a plain colored title line -- shown inline, with no
// screen clear -- otherwise. Returned as a string (see
// finishHeaderLine) rather than printed directly so a caller like
// drawAreaLightbar can both avoid doubling up the trailing blank line
// and count the header's own line count (customizable per deployment,
// so not something to hardcode) toward a scrollable list's viewport
// budget, the same reason renderMessageListHeader/
// renderMessageReaderHeader return strings.
//
// The builtin command signature (see the builtins map) doesn't carry
// a node number, so only the node-independent placeholders are
// available here -- BBSNAME, SYSOP, USERNAME, SL. That covers every
// placeholder a sensible area-header design would want; NODE/DATE/
// TIME/VERSION/TOTALCALLS aren't available in this context.
func (s *Server) renderAreaHeader(term *Terminal, u *user.User, screenFile, fallbackTitle string) string {
	raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, screenFile))
	if err != nil {
		return finishHeaderLine(ansi.Reset + "\n" + ansi.FG(ansi.Cyan, true) + fallbackTitle + ansi.Reset)
	}
	vars := ansi.Vars{
		"BBSNAME":  s.BBSName,
		"SYSOP":    s.SysopName,
		"USERNAME": u.Username,
		"SL":       strconv.Itoa(u.SecurityLevel),
	}
	rendered := ansi.Render(raw, vars)
	return finishHeaderLine(ansi.Layout(rendered, term.Width()))
}

func renderMenu(m *menu.Menu, securityLevel int, vars ansi.Vars) string {
	var b strings.Builder
	b.WriteString(ansi.Reset + "\n" + ansi.FG(ansi.Green, true) + ansi.Render(m.Title, vars) + ansi.Reset + "\n")
	for _, item := range m.VisibleItems(securityLevel) {
		label := ansi.Render(item.Label, vars)
		fmt.Fprintf(&b, "  [%s%s%s] %s\n", ansi.FG(ansi.Yellow, true), item.Key, ansi.FG(ansi.Green, true), label)
	}
	b.WriteString("\n" + ansi.FG(ansi.White, true) + vars["USERNAME"] + "> " + ansi.Reset)
	return b.String()
}

// pauseForKey prompts for and waits on an acknowledgment before
// returning. It exists because the menu loop redisplays the current
// menu (screen-clearing, if the menu has a hand-designed Screen)
// right after a builtin returns -- without a pause, purely
// informational output like a stats or who's-online listing would be
// wiped by that redraw before a caller could ever read it.
func (s *Server) pauseForKey(term *Terminal) error {
	if err := term.Print("\n" + ansi.FG(ansi.White, true) + "Press Enter to continue..." + ansi.Reset); err != nil {
		return err
	}
	_, err := term.ReadLine(false)
	return err
}

func (s *Server) showVersion(term *Terminal, u *user.User) error {
	if err := term.Println("\n" + Version); err != nil {
		return err
	}
	return s.pauseForKey(term)
}

func (s *Server) showStats(term *Terminal, u *user.User) error {
	if err := term.Println("\n" + ansi.FG(ansi.Cyan, true) + "Your account" + ansi.Reset); err != nil {
		return err
	}
	if err := term.Println(fmt.Sprintf("Handle:         %s", u.Username)); err != nil {
		return err
	}
	if err := term.Println(fmt.Sprintf("Security level: %d", u.SecurityLevel)); err != nil {
		return err
	}
	if err := term.Println(fmt.Sprintf("Total calls:    %d", u.TotalCalls)); err != nil {
		return err
	}
	if err := term.Println(fmt.Sprintf("Member since:   %s", u.CreatedAt.Format("2006-01-02"))); err != nil {
		return err
	}
	return s.pauseForKey(term)
}

// sysopListUsers is the "builtin:listusers" command, reachable only
// through a menu item gated at sysop level (see configs/menus/sysop.yaml).
func (s *Server) sysopListUsers(term *Terminal, _ *user.User) error {
	users, err := s.Users.ListAll()
	if err != nil {
		return err
	}
	if err := term.Println("\n" + ansi.FG(ansi.Cyan, true) + "Username             SL   Calls  Last login" + ansi.Reset); err != nil {
		return err
	}
	for _, listed := range users {
		lastLogin := "never"
		if listed.LastLoginAt.Valid {
			lastLogin = listed.LastLoginAt.Time.Format("2006-01-02 15:04 UTC")
		}
		line := fmt.Sprintf("%-21s%-5d%-7d%s", listed.Username, listed.SecurityLevel, listed.TotalCalls, lastLogin)
		if err := term.Println(line); err != nil {
			return err
		}
	}
	return s.pauseForKey(term)
}

// sysopSetSecurityLevel is the "builtin:setsl" command: it prompts for
// a target username and a new SL (0-255) and applies it via
// user.Store.SetSecurityLevel.
func (s *Server) sysopSetSecurityLevel(term *Terminal, sysop *user.User) error {
	if err := term.Print(ansi.Reset + "\nUsername to modify: " + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	target, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return term.Println(ansi.Reset + "Cancelled.")
	}

	tu, err := s.Users.ByUsername(target)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "No such user.")
		}
		return err
	}

	if err := term.Println(ansi.Reset + fmt.Sprintf("Current security level for %s: %d", tu.Username, tu.SecurityLevel)); err != nil {
		return err
	}
	level, err := s.promptSecurityLevel(term, "New security level (0-255): ")
	if err != nil {
		return err
	}
	if level < 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Invalid security level.")
	}

	if err := s.Users.SetSecurityLevel(tu.ID, level); err != nil {
		if errors.Is(err, user.ErrLastSysop) {
			return term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + "Cannot demote the last sysop-level account.")
		}
		return err
	}
	s.logInfo("%s set %s's security level to %d", sysop.Username, tu.Username, level)
	return term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + fmt.Sprintf("%s is now SL %d.", tu.Username, level))
}

func (s *Server) showWho(term *Terminal, _ *user.User) error {
	nodes, err := s.Nodes.List()
	if err != nil {
		return err
	}
	if err := term.Println("\n" + ansi.FG(ansi.Cyan, true) + "Node  Handle               Terminal    Connected" + ansi.Reset); err != nil {
		return err
	}
	for _, n := range nodes {
		if err := term.Println(fmt.Sprintf("%-6d%-21s%-12s%s", n.Node, n.Username, n.TermType, n.ConnectedAt.Format("15:04:05 UTC"))); err != nil {
			return err
		}
	}
	return s.pauseForKey(term)
}
