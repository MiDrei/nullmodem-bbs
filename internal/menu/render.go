package menu

import (
	"fmt"
	"regexp"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"
)

// RenderGenerated is the menu as text, for a menu without a screen of
// its own (or a missing one): the title, then a line per item the
// caller may see, then the prompt.
func RenderGenerated(m *Menu, securityLevel int, vars ansi.Vars) string {
	var b strings.Builder
	b.WriteString(ansi.Reset + "\n" + ansi.FG(ansi.Green, true) + ansi.Render(m.Title, vars) + ansi.Reset + "\n")
	for _, item := range m.VisibleItems(securityLevel) {
		label := ansi.Render(item.Label, vars)
		fmt.Fprintf(&b, "  [%s%s%s] %s\n", ansi.FG(ansi.Yellow, true), item.Key, ansi.FG(ansi.Green, true), label)
	}
	b.WriteString("\n" + ansi.FG(ansi.White, true) + vars["USERNAME"] + "> " + ansi.Reset)
	return b.String()
}

// NotShown returns the items (of those visible) a screen's text
// doesn't seem to mention -- neither as "[K]", "(K)", "K)", "<K>" nor
// by their label: a hand-designed screen doesn't list items by
// itself, so a new one is easily forgotten there.
func NotShown(m *Menu, securityLevel int, screenText string, vars ansi.Vars) []Item {
	text := strings.ToLower(screenText)
	var out []Item
	for _, it := range m.VisibleItems(securityLevel) {
		k := strings.ToLower(it.Key)
		label := strings.ToLower(strings.TrimSpace(ansi.Render(it.Label, vars)))
		found := false
		for _, form := range []string{"[" + k + "]", "(" + k + ")", "<" + k + ">", " " + k + ")", "\n" + k + ")", " " + k + " -", " " + k + " :"} {
			if strings.Contains(text, form) {
				found = true
				break
			}
		}
		if !found && label != "" && strings.Contains(text, label) {
			found = true
		}
		if !found {
			out = append(out, it)
		}
	}
	return out
}

// SysopMenuSL is the level the stock main menu opens the sysop menu
// at (configs/menus/main.yaml's S item).
const SysopMenuSL = 200

// SysopItem is the {SYSOP_ITEM} placeholder: the sysop menu's entry on
// the main menu screen, for those who may use it only.
// label is the entry's text ("Sysop Menu") in the caller's language.
func SysopItem(securityLevel int, label string) string {
	if securityLevel < SysopMenuSL {
		return ""
	}
	return ansi.FG(ansi.Yellow, true) + "[S]" + ansi.FG(ansi.Green, true) + " " + label
}

var bracketKey = regexp.MustCompile(`\[([A-Za-z0-9?!#*+-])\]`)

// OnlyOnScreen returns the keys a screen shows as "[K]" that no item
// a caller at securityLevel may use answers to: pressing them only
// gets "Unknown command."
func OnlyOnScreen(m *Menu, securityLevel int, screenText string) []string {
	have := map[string]bool{}
	for _, it := range m.VisibleItems(securityLevel) {
		have[strings.ToUpper(it.Key)] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, match := range bracketKey.FindAllStringSubmatch(screenText, -1) {
		k := strings.ToUpper(match[1])
		if !have[k] && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
