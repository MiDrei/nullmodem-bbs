package chat

import "strings"

// SourceMatrix marks a line said on Matrix (internal/matrix).
const SourceMatrix = "matrix"

// Bridged reports whether a line came over a bridge (Discord, Matrix).
func Bridged(source string) bool { return source == SourceDiscord || source == SourceMatrix }

// Speaker is how a line's author shows: "name@discord" for one who
// spoke on a bridged network, the handle otherwise.
func Speaker(l Line) string {
	if Bridged(l.Source) {
		return l.Username + "@" + l.Source
	}
	return l.Username
}

// BridgeLine is what a room's line says on the bridged network
// (SourceDiscord, SourceMatrix), under which name; false for lines that
// stay here: ones that came from there, pages, and -- quiet -- who
// came and went. bbsName speaks for the board.
func BridgeLine(l Line, network string, quiet bool, bbsName string) (name, text string, ok bool) {
	if l.Source == network {
		return "", "", false // came from there
	}
	if bbsName == "" {
		bbsName = "BBS"
	}
	switch l.Kind {
	case Say:
		if strings.TrimSpace(l.Text) == "" {
			return "", "", false
		}
		return Speaker(l), l.Text, true
	case Join, Leave:
		if quiet || Bridged(l.Source) {
			return "", "", false
		}
		via := "on the BBS"
		if l.Source == "web" {
			via = "on the web"
		}
		verb := "joined"
		if l.Kind == Leave {
			verb, via = "left", ""
		}
		return bbsName, "*" + strings.TrimSpace(l.Username+" "+verb+" "+via) + "*", true
	}
	return "", "", false
}
