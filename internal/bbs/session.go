package bbs

import (
	"context"
	"errors"
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/community"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/applog"
	"git.maik.ch/nullmodem/bbs/internal/doors"
	"git.maik.ch/nullmodem/bbs/internal/file"
	"git.maik.ch/nullmodem/bbs/internal/guard"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/menu"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/nodelist"
	"git.maik.ch/nullmodem/bbs/internal/session"
	"git.maik.ch/nullmodem/bbs/internal/stats"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/bbs/internal/version"
	"git.maik.ch/nullmodem/kit/ansi"
)

// Version is the BBS software version shown on the welcome screen and
// the [V]ersion menu command.
const Version = version.Version

// maxLoginAttempts is how many wrong passwords a session may try
// before being disconnected.
const maxLoginAttempts = 3

// Server drives BBS sessions handed to it by any transport (telnet,
// SSH, ...) that implements Conn.
type Server struct {
	Nodes    *session.Store
	Users    *user.Store
	Menus    menu.Getter
	Messages *message.Store
	Files    *file.Store
	Netmail  *netmail.Store
	Doors    []doors.Door
	// LoadDoors, if set, supplies the current door list each time the
	// doors menu opens, instead of the fixed Doors above -- see
	// cmd/bbs's loadDoors.
	LoadDoors     func() []doors.Door
	Logger        *applog.Logger
	SysopName     string
	BBSName       string
	FTNAddress    string
	NewUserSL     int
	WelcomeScreen string
	ScreensDir    string
	// LastCallers is the InterBBS Last Callers setup (see
	// lastcallers.go); zero means off.
	LastCallers config.LastCallersConfig
	// Guard, if set, locks out IPs that keep failing to log in.
	Guard *guard.Guard
	// Security, if set, supplies the current lockout and approval
	// settings (re-read from the config now and then); nil: no
	// connection limit, no approval.
	Security func() config.SecurityConfig
	conns    guard.Conns
	// FullScreenEditor: messages are written full screen (fse.go),
	// unless a caller chose the line editor in their profile.
	FullScreenEditor bool
	// Chat holds the chat rooms and the one-liners (community.go);
	// nil turns them off.
	Chat     *chat.Store
	nodeMsgs nodeMessages
	// Nodelist answers who's behind an FTN address; nil: no nodelists.
	Nodelist *nodelist.Store
	// Community holds the polls and the BBS list.
	Community *community.Store
	// Stats records calls and door sessions; nil records nothing.
	Stats *stats.Store
	// Language, if set, is the board's language (re-read from the
	// config): what callers read before logging in, and after when
	// they never chose one. nil: English.
	Language func() string
	// Email, if set, is the email gateway's settings (re-read from the
	// config); nil: no gateway.
	Email func() config.EmailConfig
	// SecurityLevels, if set, is the named security levels (see
	// config.Config.Levels; name names the board's own ones).
	SecurityLevels func(name func(key string) string) []config.SecurityLevel
}

// Options bundles the dependencies and configuration NewServer needs.
// It exists mainly so adding a new setting (like WelcomeScreen) doesn't
// require touching every call site's positional argument list.
type Options struct {
	BBSName          string
	SysopName        string
	FTNAddress       string
	Users            *user.Store
	Menus            menu.Getter
	Messages         *message.Store
	Files            *file.Store
	Netmail          *netmail.Store
	Doors            []doors.Door
	LoadDoors        func() []doors.Door
	Nodes            *session.Store
	Logger           *applog.Logger
	NewUserSL        int
	WelcomeScreen    string
	ScreensDir       string
	LastCallers      config.LastCallersConfig
	Guard            *guard.Guard
	Security         func() config.SecurityConfig
	FullScreenEditor bool
	Chat             *chat.Store
	Nodelist         *nodelist.Store
	Community        *community.Store
	Stats            *stats.Store
	Language         func() string
	Email            func() config.EmailConfig
	SecurityLevels   func(name func(key string) string) []config.SecurityLevel
}

// NewServer returns a Server ready to accept sessions.
func NewServer(opts Options) *Server {
	return &Server{
		Nodes:            opts.Nodes,
		Users:            opts.Users,
		Menus:            opts.Menus,
		Messages:         opts.Messages,
		Files:            opts.Files,
		Netmail:          opts.Netmail,
		Doors:            opts.Doors,
		LoadDoors:        opts.LoadDoors,
		Logger:           opts.Logger,
		BBSName:          opts.BBSName,
		SysopName:        opts.SysopName,
		FTNAddress:       opts.FTNAddress,
		NewUserSL:        opts.NewUserSL,
		WelcomeScreen:    opts.WelcomeScreen,
		ScreensDir:       opts.ScreensDir,
		LastCallers:      opts.LastCallers,
		Guard:            opts.Guard,
		Security:         opts.Security,
		FullScreenEditor: opts.FullScreenEditor,
		Chat:             opts.Chat,
		Nodelist:         opts.Nodelist,
		Community:        opts.Community,
		Stats:            opts.Stats,
		Language:         opts.Language,
		Email:            opts.Email,
		SecurityLevels:   opts.SecurityLevels,
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
	// Locked out or too many connections: told so, and gone -- before
	// taking a node.
	ip := guard.IP(conn.RemoteAddr().String())
	if s.Guard != nil {
		if v, err := s.Guard.Check(ip); err == nil && v.Blocked {
			s.logWarn("[%s] refused %s: locked out", conn.Protocol(), ip)
			conn.Write([]byte("\r\n" + toCP437(v.MessageIn(s.boardLang())) + "\r\n"))
			time.Sleep(300 * time.Millisecond)
			return
		}
	}
	if s.Security != nil {
		if !s.conns.Open(ip, s.Security().MaxConnections()) {
			s.logWarn("[%s] refused %s: too many connections from it", conn.Protocol(), ip)
			conn.Write([]byte("\r\n" + toCP437(i18n.T(s.boardLang(), "guard.too_many")) + "\r\n"))
			time.Sleep(300 * time.Millisecond)
			return
		}
		defer s.conns.Close(ip)
	}

	node, err := s.Nodes.Join(conn.RemoteAddr().String(), conn.TermType())
	if err != nil {
		return
	}
	// protocol prefixes every log line for this connection's lifetime
	// (a plain, greppable "[telnet]"/"[ssh]" tag rather than a new DB
	// column) so the admin Logs page can split the two apart -- both
	// share source "bbs" (cmd/bbs's one applog.Logger), which alone
	// can't tell them apart.
	protocol := conn.Protocol()
	s.logInfo("[%s] node %d connected from %s (%s)", protocol, node, conn.RemoteAddr(), conn.TermType())
	defer func() {
		s.logInfo("[%s] node %d disconnected", protocol, node)
		s.Nodes.Leave(node)
	}()

	term := NewTerminal(conn)
	term.Node = node
	term.RemoteIP = ip
	term.Protocol = protocol
	term.Lang = s.boardLang()
	defer func() { recover() }()

	// Hang up on a caller who stopped typing: a short while at the
	// login, the board's idle limit once logged in.
	idleCtx, stopIdle := context.WithCancel(context.Background())
	defer stopIdle()
	var loggedIn atomic.Bool
	go term.watchIdle(idleCtx, func() time.Duration {
		if !loggedIn.Load() {
			return LoginIdleLimit
		}
		if s.Security == nil {
			return 30 * time.Minute
		}
		return time.Duration(s.Security().Idle()) * time.Minute
	}, func() {
		s.logInfo("[%s] node %d: hung up for inactivity", protocol, node)
		conn.Write([]byte("\r\n\r\n" + ansi.Reset + toCP437(term.T("session.idle_logoff")) + "\r\n"))
		conn.Close()
	})

	if err := s.welcome(term, node); err != nil {
		return
	}
	if err := s.askLoginLanguage(term); err != nil {
		return
	}

	u, err := s.login(term)
	if err != nil {
		return
	}
	loggedIn.Store(true)
	s.Nodes.SetUsername(node, u.Username)
	term.SetLocation(u.Location())
	term.Lang = s.userLang(u)
	s.logInfo("[%s] node %d: %s logged in", protocol, node, u.Username)
	if err := s.Stats.RecordCall(u.ID, protocol); err != nil {
		s.logWarn("%v", err)
	}
	// Whatever way the call ends, it's this board's newest last caller.
	defer s.postLastCaller(u)

	if s.LastCallers.ShowAtLogin {
		if err := s.showLastCallers(term, u); err != nil {
			return
		}
	}
	if err := s.showOneliners(term, u); err != nil {
		return
	}
	if err := s.showNewsAtLogin(term, u); err != nil {
		return
	}
	if err := s.loginSummary(term, u); err != nil {
		return
	}

	if err := s.runMenu(term, u, node, "main"); err != nil && !errors.Is(err, errLogoff) {
		s.logWarn("[%s] node %d (%s): menu error: %v", protocol, node, u.Username, err)
		term.Println("\n" + ansi.FG(ansi.Red, true) + term.T("menu.error", "ERROR", err.Error()))
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
	// welcome.ans as it is now (edited in the designer), else the one
	// read at startup.
	screen := s.WelcomeScreen
	if s.ScreensDir != "" {
		if raw, err := s.loadScreen(term.Lang, "welcome.ans"); err == nil {
			screen = raw
		}
	}
	rendered := ansi.Render(screen, s.baseVars(node))
	return term.Print(ansi.Layout(rendered, term.Width()))
}

// baseVars are the placeholders available before login, when there is
// no authenticated user yet to describe.
func (s *Server) baseVars(node int) ansi.Vars {
	now := time.Now()
	return ansi.Vars{
		"BBSNAME": s.BBSName,
		"SYSOP":   s.SysopName,
		"VERSION": version.Full(),
		"NODE":    strconv.Itoa(node),
		"DATE":    now.Format("2006-01-02"),
		"TIME":    now.Format("15:04:05"),
	}
}

// userVars extends baseVars with placeholders describing the
// authenticated caller, for use once login has completed.
func (s *Server) userVars(term *Terminal, u *user.User, node int) ansi.Vars {
	vars := s.baseVars(node)
	now := time.Now().In(u.Location())
	vars["DATE"] = now.Format("2006-01-02")
	vars["TIME"] = now.Format("15:04:05")
	vars["USERNAME"] = u.Username
	vars["SL"] = strconv.Itoa(u.SecurityLevel)
	vars["TOTALCALLS"] = strconv.Itoa(u.TotalCalls)
	// The sysop menu's entry, shown to those who may use it only.
	vars["SYSOP_ITEM"] = menu.SysopItem(u.SecurityLevel, term.T("menu.sysop_item"))
	return vars
}

// login prompts for a username, then either authenticates an existing
// account or walks a first-time caller through registration.
func (s *Server) login(term *Terminal) (*user.User, error) {
	for {
		if err := term.Print(ansi.Reset + "\n" + term.T("login.handle") + ansi.FG(ansi.Yellow, true)); err != nil {
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

		if user.IsRestrictedUsername(handle) || s.blockedHandle(handle) {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("login.handle_reserved")); err != nil {
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
		if err := term.Print(ansi.Reset + term.T("login.password") + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, err
		}
		password, err := term.ReadLine(true)
		if err != nil {
			return nil, err
		}

		u, err := s.Users.Authenticate(handle, password)
		if err == nil {
			if s.Guard != nil {
				s.Guard.Succeed(term.RemoteIP)
			}
			return u, nil
		}
		if !errors.Is(err, user.ErrInvalidCredentials) {
			return nil, err
		}
		if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("login.wrong_password")); err != nil {
			return nil, err
		}
		if s.Guard != nil {
			if v, err := s.Guard.Fail(term.RemoteIP, handle, term.Protocol); err == nil && v.Blocked {
				term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + toCP437(v.MessageIn(term.Lang)) + ansi.Reset)
				return nil, nil
			}
		}
	}
	return nil, nil
}

// registerNew offers to create a new account for a handle that does
// not exist yet. It returns ok=false if the caller declines, so login
// can loop back to the handle prompt.
func (s *Server) registerNew(term *Terminal, handle string) (*user.User, bool, error) {
	if err := term.Println(ansi.Reset + "\n" + ansi.FG(ansi.Green, true) + term.T("register.new_handle", "HANDLE", handle)); err != nil {
		return nil, false, err
	}
	if err := term.Print(term.T("register.create") + " " + ansi.FG(ansi.Yellow, true)); err != nil {
		return nil, false, err
	}
	answer, err := term.ReadLine(false)
	if err != nil {
		return nil, false, err
	}
	if a := strings.ToUpper(strings.TrimSpace(answer)); a != "" && !isYes(term, a) {
		return nil, false, nil
	}
	lang := term.Lang
	if !term.LangChosen {
		if lang, err = s.chooseLanguage(term); err != nil {
			return nil, false, err
		}
		term.Lang = lang
	}

	for {
		if err := term.Print(ansi.Reset + term.T("register.password", "MIN", user.MinPasswordLength) + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, false, err
		}
		pw1, err := term.ReadLine(true)
		if err != nil {
			return nil, false, err
		}
		if len(pw1) < user.MinPasswordLength {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("register.password_short")); err != nil {
				return nil, false, err
			}
			continue
		}

		if err := term.Print(ansi.Reset + term.T("register.password_confirm") + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, false, err
		}
		pw2, err := term.ReadLine(true)
		if err != nil {
			return nil, false, err
		}
		if pw1 != pw2 {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("register.password_mismatch")); err != nil {
				return nil, false, err
			}
			continue
		}

		realName, err := s.promptRealName(term)
		if err != nil {
			return nil, false, err
		}

		sl, pending := s.NewUserSL, false
		if s.Security != nil && s.Security().Approval() {
			sl, pending = s.Security().Pending(), true
		}
		u, err := s.Users.RegisterNew(handle, pw1, sl, pending)
		if err != nil {
			return nil, false, err
		}
		if err := s.Users.SetRealName(u.ID, realName); err != nil {
			return nil, false, err
		}
		u.RealName = realName
		if err := s.Users.SetLanguage(u.ID, lang); err != nil {
			return nil, false, err
		}
		u.Language = lang
		if u.Validated {
			s.logInfo("new account registered: %s (SL %d)", u.Username, u.SecurityLevel)
		} else {
			s.logInfo("new account registered: %s (SL %d), waiting for approval", u.Username, u.SecurityLevel)
		}
		if err := term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("register.created", "HANDLE", handle)); err != nil {
			return nil, false, err
		}
		if !u.Validated {
			if err := term.Println(ansi.FG(ansi.Yellow, true) + term.T("approval.pending") + ansi.Reset); err != nil {
				return nil, false, err
			}
		}
		if u.SecurityLevel >= user.SLSysop {
			if err := term.Println(ansi.FG(ansi.Yellow, true) + term.T("register.first_sysop")); err != nil {
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
		if err := term.Print(ansi.Reset + term.T("register.real_name") + ansi.FG(ansi.Yellow, true)); err != nil {
			return "", err
		}
		realName, err := term.ReadLine(false)
		if err != nil {
			return "", err
		}
		realName = strings.TrimSpace(realName)
		if err := user.ValidateRealName(realName); err != nil {
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + realNameErrorText(term, err)); err != nil {
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
// and referencing "builtin:<name>" from a menu YAML file. "stats" is
// the pre-profile name of the same [Y] entry, kept so a deployment's
// own (bind-mounted, never overwritten) main.yaml still reaches the
// profile.
var builtins = map[string]func(s *Server, term *Terminal, u *user.User) error{
	"who":            (*Server).showWho,
	"profile":        (*Server).showProfile,
	"stats":          (*Server).showProfile,
	"version":        (*Server).showVersion,
	"lastcallers":    (*Server).showLastCallers,
	"listusers":      (*Server).sysopListUsers,
	"setsl":          sysopDialog("syssetsl.ans", "screen.sysop.setsl", (*Server).sysopSetSecurityLevel),
	"areas":          (*Server).showAreas,
	"createarea":     sysopDialog("sysarea.ans", "screen.sysop.createarea", (*Server).sysopCreateArea),
	"files":          (*Server).showFileAreas,
	"createfilearea": sysopDialog("sysfilearea.ans", "screen.sysop.createfilearea", (*Server).sysopCreateFileArea),
	"importfile":     sysopDialog("sysimport.ans", "screen.sysop.importfile", (*Server).sysopImportFile),
	"netmail":        (*Server).showNetmail,
	"doors":          (*Server).showDoors,
	"qwk":            paused((*Server).downloadQWK),
	"qwkrep":         paused((*Server).uploadQWKReply),
	"qwkareas":       (*Server).configureQWKAreas,
	"newscan":        (*Server).newScan,
	"tome":           (*Server).toMe,
	"chat":           (*Server).teleconference,
	"page":           (*Server).pageSysop,
	"oneliners":      (*Server).showOneliners,
	"nodelist":       (*Server).browseNodelist,
	"polls":          (*Server).votingBooth,
	"newfiles":       (*Server).newFiles,
	"filesearch":     (*Server).searchFiles,
	"bbslist":        (*Server).bbsList,
	"news":           (*Server).showNews,
}

// paused runs a builtin and then waits for a key, so what it printed
// last isn't wiped at once by the menu's screen.
func paused(f func(s *Server, term *Terminal, u *user.User) error) func(s *Server, term *Terminal, u *user.User) error {
	return func(s *Server, term *Terminal, u *user.User) error {
		if err := f(s, term, u); err != nil {
			return err
		}
		return s.pauseForKey(term)
	}
}

// sysopDialog runs one of the sysop's question-and-answer functions
// on a cleared screen under its banner (screenFile, else the title
// titleKey names), waiting for a key after it so its result can be
// read before the menu comes back.
func sysopDialog(screenFile, titleKey string, f func(s *Server, term *Terminal, u *user.User) error) func(s *Server, term *Terminal, u *user.User) error {
	return func(s *Server, term *Terminal, u *user.User) error {
		if err := term.Print(s.featureHeader(term, u, screenFile, term.T(titleKey))); err != nil {
			return err
		}
		return paused(f)(s, term, u)
	}
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
		if err := s.showNodeMessages(term); err != nil {
			return err
		}
		rendered := s.renderMenuDisplay(term, m, u, node)
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
			if err := term.Println("\n" + term.T("menu.unknown")); err != nil {
				return err
			}
			continue
		}

		switch {
		case item.Action == "back":
			// Back to the menu this one was opened from (main, at the top).
			return nil

		case item.Action == "logoff":
			if err := s.printLogoffScreen(term, u, node); err != nil {
				return err
			}
			return errLogoff

		case strings.HasPrefix(item.Action, "goto:"):
			target := strings.TrimPrefix(item.Action, "goto:")
			if target == "sysop" {
				if ok, err := s.sysopGate(term, u); err != nil {
					return err
				} else if !ok {
					continue
				}
			}
			if err := s.runMenu(term, u, node, target); err != nil {
				return err
			}

		case strings.HasPrefix(item.Action, "builtin:"):
			name := strings.TrimPrefix(item.Action, "builtin:")
			if sysopBuiltins[name] {
				if ok, err := s.sysopGate(term, u); err != nil {
					return err
				} else if !ok {
					continue
				}
			}
			fn, ok := builtins[name]
			if !ok {
				if err := term.Println("\n" + term.T("menu.unimplemented", "NAME", name)); err != nil {
					return err
				}
				continue
			}
			if err := fn(s, term, u); err != nil {
				return err
			}

		default:
			if err := term.Println("\n" + term.T("menu.bad_action", "ACTION", item.Action)); err != nil {
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
func (s *Server) renderMenuDisplay(term *Terminal, m *menu.Menu, u *user.User, node int) string {
	vars := s.userVars(term, u, node)
	if m.Screen != "" {
		raw, err := s.loadScreen(term.Lang, m.Screen)
		if err != nil {
			s.logWarn("menu %q: could not load screen %q: %v", m.Name, m.Screen, err)
		} else {
			return ansi.Render(menu.SysopLines(raw, u.SecurityLevel), vars)
		}
	}
	return renderMenu(m.In(term.Lang), u.SecurityLevel, vars)
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
	raw, err := s.loadScreen(term.Lang, logoffScreenFile)
	if err != nil {
		return term.Println("\n" + term.T("logoff.goodbye", "USERNAME", u.Username))
	}
	rendered := ansi.Render(menu.SysopLines(raw, u.SecurityLevel), s.userVars(term, u, node))
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
	raw, err := s.loadScreen(term.Lang, screenFile)
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

// featureHeader is the banner above one of the board's features (who's
// online, the last callers, the one-liners, ...): screenFile filled in
// when there is one, else the plain title -- always on a cleared
// screen, so a feature starts at the top whatever came before it.
func (s *Server) featureHeader(term *Terminal, u *user.User, screenFile, title string) string {
	raw, err := s.loadScreen(term.Lang, screenFile)
	if err != nil {
		return ansi.ClearScreen() + ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + "  " + title + ansi.Reset + "\r\n"
	}
	vars := ansi.Vars{"BBSNAME": s.BBSName, "SYSOP": s.SysopName, "TITLE": title}
	if u != nil {
		vars["USERNAME"], vars["SL"] = u.Username, strconv.Itoa(u.SecurityLevel)
	}
	rendered := ansi.Render(ansi.StripLeadingScreenClear(raw), vars)
	head := finishHeaderLine(ansi.Layout(rendered, term.Width()))
	// What follows starts right under the title line: the banners'
	// own blank line after it is dropped.
	if strings.HasSuffix(head, "\r\n\r\n") {
		head = strings.TrimSuffix(head, "\r\n")
	}
	return ansi.ClearScreen() + head
}

func renderMenu(m *menu.Menu, securityLevel int, vars ansi.Vars) string {
	return menu.RenderGenerated(m, securityLevel, vars)
}

// pauseForKey prompts for and waits on an acknowledgment before
// returning. It exists because the menu loop redisplays the current
// menu (screen-clearing, if the menu has a hand-designed Screen)
// right after a builtin returns -- without a pause, purely
// informational output like a stats or who's-online listing would be
// wiped by that redraw before a caller could ever read it.
func (s *Server) pauseForKey(term *Terminal) error {
	if err := term.Print("\n" + keyHints(term.T("common.press_enter")) + ansi.Reset); err != nil {
		return err
	}
	_, err := term.ReadLine(false)
	return err
}

func (s *Server) showVersion(term *Terminal, u *user.User) error {
	if err := term.Println("\n" + version.Full()); err != nil {
		return err
	}
	return s.pauseForKey(term)
}

// sysopListUsers is the "builtin:listusers" command, reachable only
// through a menu item gated at sysop level (see configs/menus/sysop.yaml).
func (s *Server) sysopListUsers(term *Terminal, u *user.User) error {
	users, err := s.Users.ListAll()
	if err != nil {
		return err
	}
	// A page at a time, as many as fit under the banner: Enter the
	// next, Q enough.
	header := s.featureHeader(term, u, "sysusers.ans", term.T("screen.sysop.listusers"))
	page := max(3, term.Height()-strings.Count(header, "\n")-5)
	for start := 0; ; start += page {
		end := min(start+page, len(users))
		var b strings.Builder
		b.WriteString(header)
		b.WriteString("  " + fgDim(ansi.White) + padCP(term.T("sysop.users_col_user"), 21) + padCP(term.T("sysop.users_col_sl"), 5) + padCP(term.T("sysop.users_col_calls"), 8) + term.T("sysop.users_col_last") + "\r\n" +
			fgDim(ansi.Blue) + "  " + strings.Repeat("\xc4", 76) + ansi.Reset + "\r\n")
		for _, listed := range users[start:end] {
			lastLogin := term.T("common.never")
			if listed.LastLoginAt.Valid {
				lastLogin = term.Time(listed.LastLoginAt.Time).Format("2006-01-02 15:04 MST")
			}
			fmt.Fprintf(&b, "  %s%-21s%s%-5d%s%-8d%s%s%s\r\n", ansi.FG(ansi.White, true), listed.Username, ansi.FG(ansi.Cyan, true), listed.SecurityLevel,
				fgDim(ansi.White), listed.TotalCalls, ansi.FG(ansi.Black, true), lastLogin, ansi.Reset)
		}
		b.WriteString("\r\n" + ansi.FG(ansi.Black, true) + "-- " + term.T("list.range", "FROM", start+1, "TO", end, "TOTAL", len(users)) + " --" + ansi.Reset)
		if end >= len(users) {
			if err := term.Print(b.String()); err != nil {
				return err
			}
			return s.pauseForKey(term)
		}
		b.WriteString("\r\n" + keyHints(term.T("sysop.users_more")) + ansi.Reset + " ")
		if err := term.Print(b.String()); err != nil {
			return err
		}
		in, err := term.ReadLine(false)
		if err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(in), "q") {
			return nil
		}
	}
}

// sysopSetSecurityLevel is the "builtin:setsl" command: it prompts for
// a target username and a new SL (0-255) and applies it via
// user.Store.SetSecurityLevel.
func (s *Server) sysopSetSecurityLevel(term *Terminal, sysop *user.User) error {
	if err := term.Print(ansi.Reset + "  " + fgDim(ansi.White) + term.T("sysop.setsl_user") + ansi.FG(ansi.Yellow, true)); err != nil {
		return err
	}
	target, err := term.ReadLine(false)
	if err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return term.Println(ansi.Reset + "  " + term.T("common.cancelled"))
	}

	tu, err := s.Users.ByUsername(target)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return term.Println(ansi.Reset + "\r\n  " + ansi.FG(ansi.Red, true) + term.T("common.no_such_user"))
		}
		return err
	}

	if err := term.Println(ansi.Reset + "  " + fgDim(ansi.White) + term.T("sysop.setsl_current", "USERNAME", tu.Username, "SL", tu.SecurityLevel)); err != nil {
		return err
	}
	// The named levels to choose from.
	if s.SecurityLevels != nil {
		var b strings.Builder
		for _, l := range s.SecurityLevels(func(k string) string { return term.U(k) }) {
			fmt.Fprintf(&b, "  %s%5d%s  %s\r\n", ansi.FG(ansi.Cyan, true), l.Level, ansi.FG(ansi.White, true), toCP437(l.Name))
		}
		if err := term.Print("\r\n" + b.String() + ansi.Reset + "\r\n"); err != nil {
			return err
		}
	}
	level, err := s.promptSecurityLevel(term, term.T("sysop.setsl_new"))
	if err != nil {
		return err
	}
	if level < 0 {
		return term.Println(ansi.Reset + "\r\n  " + ansi.FG(ansi.Red, true) + term.T("sysop.setsl_invalid"))
	}

	if err := s.Users.SetSecurityLevel(tu.ID, level); err != nil {
		if errors.Is(err, user.ErrLastSysop) {
			return term.Println(ansi.Reset + "\r\n  " + ansi.FG(ansi.Red, true) + term.T("sysop.setsl_last_sysop"))
		}
		return err
	}
	s.logInfo("%s set %s's security level to %d", sysop.Username, tu.Username, level)
	return term.Println(ansi.Reset + "\r\n  " + ansi.FG(ansi.Green, true) + term.T("sysop.setsl_done", "USERNAME", tu.Username, "SL", level))
}

func (s *Server) showWho(term *Terminal, u *user.User) error {
	nodes, err := s.Nodes.List()
	if err != nil {
		return err
	}
	if err := term.Print(s.featureHeader(term, u, "who.ans", term.T("common.who_s_online"))); err != nil {
		return err
	}
	if err := term.Println(fgDim(ansi.White) + "  " + padCP(term.T("common.node"), 6) + padCP(term.T("common.handle"), 21) + padCP(term.T("common.terminal"), 12) + term.T("common.connected") + "\r\n" +
		fgDim(ansi.Blue) + "  " + strings.Repeat("\xc4", 76) + ansi.Reset); err != nil {
		return err
	}
	for _, n := range nodes {
		if err := term.Println(fmt.Sprintf("  %s%-6d%s%-21s%s%-12s%s%s%s", ansi.FG(ansi.Cyan, true), n.Node, ansi.FG(ansi.White, true), n.Username,
			fgDim(ansi.White), n.TermType, ansi.FG(ansi.Black, true), term.Time(n.ConnectedAt).Format("15:04:05 MST"), ansi.Reset)); err != nil {
			return err
		}
	}
	if len(nodes) > 1 && u != nil && u.Validated {
		return s.sendNodeMessage(term, u)
	}
	return s.pauseForKey(term)
}
