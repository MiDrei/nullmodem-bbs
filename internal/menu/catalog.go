package menu

import (
	"fmt"
	"regexp"
	"strings"
)

// Builtin is a command a menu item can run ("builtin:<Name>"). The
// list is what the web admin's menu editor offers; internal/bbs's
// tests check it against the commands it actually has.
type Builtin struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Sysop: asks for the sysop's second factor first (Telnet).
	Sysop bool `json:"sysop,omitempty"`
}

// Builtins are all the commands, roughly in main-menu order.
var Builtins = []Builtin{
	{Name: "newscan", Label: "Read new messages", Description: "New messages in the caller's selected areas, one after another"},
	{Name: "tome", Label: "Messages to you", Description: "Unread messages addressed to the caller, in every area"},
	{Name: "areas", Label: "Message areas", Description: "The list of message areas: read, post, search"},
	{Name: "files", Label: "File areas", Description: "The file areas: list, download, upload, new files, search"},
	{Name: "newfiles", Label: "New files", Description: "Files the caller hasn't seen yet, in every area"},
	{Name: "filesearch", Label: "Search files", Description: "Files by name or description, in every area"},
	{Name: "netmail", Label: "Netmail", Description: "Private mail, here and across the FTN networks"},
	{Name: "chat", Label: "Chat (teleconference)", Description: "The chat rooms (/rooms, /join), bridged to Discord where set"},
	{Name: "page", Label: "Page the sysop", Description: "Calls the sysop (push, node message) and waits in a private room"},
	{Name: "oneliners", Label: "One-liners", Description: "The one-liner wall: read and add a line"},
	{Name: "who", Label: "Who's online", Description: "The nodes and who's on them; send a node message"},
	{Name: "doors", Label: "Doors", Description: "The doors (games) the caller may play"},
	{Name: "qwk", Label: "Download QWK offline mail", Description: "A QWK packet of the new messages, by Zmodem"},
	{Name: "qwkrep", Label: "Upload QWK reply packet", Description: "Replies written offline (.REP), by Zmodem"},
	{Name: "qwkareas", Label: "Area selection", Description: "Which areas the new scan and QWK packets include"},
	{Name: "profile", Label: "Your profile", Description: "Real name, time zone, password, editor, QWK settings"},
	{Name: "stats", Label: "Your profile (stats)", Description: "Same as profile"},
	{Name: "polls", Label: "Voting booth", Description: "The sysop's polls"},
	{Name: "bbslist", Label: "BBS list", Description: "The list of boards the callers keep"},
	{Name: "nodelist", Label: "Nodelist", Description: "Look up FTN systems in the imported nodelists"},
	{Name: "lastcallers", Label: "Last callers", Description: "Who called, here and across InterBBS"},
	{Name: "version", Label: "Version", Description: "The BBS software's version"},
	{Name: "listusers", Label: "List users", Description: "All accounts with their level and calls", Sysop: true},
	{Name: "setsl", Label: "Set user security level", Description: "Change an account's level", Sysop: true},
	{Name: "createarea", Label: "Create message area", Description: "A new local message area", Sysop: true},
	{Name: "createfilearea", Label: "Create file area", Description: "A new local file area", Sysop: true},
	{Name: "importfile", Label: "Import file", Description: "Add a file from the server's disk to a file area", Sysop: true},
}

// BuiltinByName finds a builtin.
func BuiltinByName(name string) (Builtin, bool) {
	for _, b := range Builtins {
		if b.Name == name {
			return b, true
		}
	}
	return Builtin{}, false
}

var (
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	keyRE  = regexp.MustCompile(`^[!-~]{1,8}$`)
)

// ValidName reports whether name may name a menu (and its file).
func ValidName(name string) bool { return nameRE.MatchString(name) }

// Check validates m for saving among the menus in set (for goto:
// targets): names, keys unique, actions that exist.
func Check(m *Menu, set Set) error {
	if !ValidName(m.Name) {
		return fmt.Errorf("a menu name is lower case letters, digits, - and _ (up to 32)")
	}
	if len(m.Items) == 0 {
		return fmt.Errorf("a menu needs at least one item")
	}
	seen := map[string]bool{}
	for _, it := range m.Items {
		key := strings.ToUpper(strings.TrimSpace(it.Key))
		if !keyRE.MatchString(key) {
			return fmt.Errorf("%q: a key is 1 to 8 characters without spaces", it.Key)
		}
		if seen[key] {
			return fmt.Errorf("the key %s is used twice", key)
		}
		seen[key] = true
		if it.MinSL < 0 || it.MinSL > 255 {
			return fmt.Errorf("%s: the security level is 0..255", key)
		}
		switch a := it.Action; {
		case a == "back", a == "logoff":
		case strings.HasPrefix(a, "builtin:"):
			if _, ok := BuiltinByName(strings.TrimPrefix(a, "builtin:")); !ok {
				return fmt.Errorf("%s: no command %q", key, strings.TrimPrefix(a, "builtin:"))
			}
		case strings.HasPrefix(a, "goto:"):
			target := strings.TrimPrefix(a, "goto:")
			if _, ok := set[target]; !ok && target != m.Name {
				return fmt.Errorf("%s: no menu %q", key, target)
			}
		default:
			return fmt.Errorf("%s: unknown action %q", key, a)
		}
	}
	return nil
}
