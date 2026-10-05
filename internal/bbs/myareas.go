package bbs

import (
	"fmt"
	"sort"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

// configureQWKAreas is the "builtin:qwkareas" command, "My areas": a
// list of every area the caller may read, by network, with a mark on
// the ones in their areas -- what the new scan (R), QWK packets and
// the reader app include. Areas added later are in by default.
// Up/Down move, Space toggles, G the whole network, A all, N none,
// S (or Enter) saves, Q/Esc leaves without saving.
func (s *Server) configureQWKAreas(term *Terminal, u *user.User) error {
	stats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		return err
	}
	if len(stats) == 0 {
		return term.Println(ansi.Reset + ansi.FG(ansi.Yellow, true) + term.T("areas.none"))
	}
	// By network (local areas first), then the area list's own order.
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i].Area, stats[j].Area
		if !strings.EqualFold(a.Network, b.Network) {
			return strings.ToLower(a.Network) < strings.ToLower(b.Network)
		}
		return a.SortOrder < b.SortOrder
	})
	out, err := s.Messages.UnsubscribedAreaIDs(u.ID)
	if err != nil {
		return err
	}
	in := map[int64]bool{}
	for _, st := range stats {
		in[st.Area.ID] = !out[st.Area.ID]
	}

	cur, top := 0, 0
	rows := max(3, term.Height()-5)
	width := term.Width()
	draw := func() error {
		if cur < top {
			top = cur
		}
		if cur >= top+rows {
			top = cur - rows + 1
		}
		n := 0
		for _, st := range stats {
			if in[st.Area.ID] {
				n++
			}
		}
		var b strings.Builder
		b.WriteString(ansi.ClearScreen() + ansi.Reset + ansi.FG(ansi.Cyan, true) + term.T("common.my_areas") + ansi.Reset +
			" -- " + term.T("myareas.count", "COUNT", n, "TOTAL", len(stats)) + "\r\n\r\n")
		nameW := max(20, width-30)
		for i := top; i < min(top+rows, len(stats)); i++ {
			st := stats[i]
			box := ansi.FG(ansi.White, false) + "[ ]"
			if in[st.Area.ID] {
				box = ansi.FG(ansi.Green, true) + "[x]"
			}
			name := []rune(st.Area.Name)
			if len(name) > nameW {
				name = name[:nameW]
			}
			network := st.Area.Network
			if network == "" {
				network = term.T("areas.local")
			}
			line := fmt.Sprintf(" %s%s %-*s %-12.12s %8s", box, ansi.Reset, nameW, toCP437(string(name)), network, newCount(term, st.New))
			if i == cur {
				line = fmt.Sprintf(" %s %-*s %-12.12s %8s", map[bool]string{true: "[x]", false: "[ ]"}[in[st.Area.ID]], nameW, toCP437(string(name)), network, newCount(term, st.New))
				line = "\x1b[47m\x1b[30m" + line + ansi.Reset
			}
			b.WriteString(line + "\r\n")
		}
		for i := min(top+rows, len(stats)) - top; i < rows; i++ {
			b.WriteString("\r\n")
		}
		b.WriteString("\r\n" + ansi.FG(ansi.White, true) + term.T("myareas.keys") + ansi.Reset)
		return term.Print(b.String())
	}
	setNetwork := func(network string) {
		all := true
		for _, st := range stats {
			if strings.EqualFold(st.Area.Network, network) && !in[st.Area.ID] {
				all = false
			}
		}
		for _, st := range stats {
			if strings.EqualFold(st.Area.Network, network) {
				in[st.Area.ID] = !all
			}
		}
	}

	for {
		if err := draw(); err != nil {
			return err
		}
		k, err := term.ReadKey()
		if err != nil {
			return err
		}
		switch {
		case k.Type == KeyUp:
			cur = max(0, cur-1)
		case k.Type == KeyDown:
			cur = min(len(stats)-1, cur+1)
		case k.Type == KeyPgUp:
			cur = max(0, cur-rows)
		case k.Type == KeyPgDn:
			cur = min(len(stats)-1, cur+rows)
		case k.Type == KeyHome:
			cur = 0
		case k.Type == KeyEnd:
			cur = len(stats) - 1
		case k.Type == KeyChar && k.Rune == ' ', k.Type == KeyChar && (k.Rune == 'x' || k.Rune == 'X'):
			in[stats[cur].Area.ID] = !in[stats[cur].Area.ID]
			cur = min(len(stats)-1, cur+1)
		case isKey(k, 'g'):
			setNetwork(stats[cur].Area.Network)
		case isKey(k, 'a'):
			for _, st := range stats {
				in[st.Area.ID] = true
			}
		case isKey(k, 'n'):
			for _, st := range stats {
				in[st.Area.ID] = false
			}
		case isKey(k, 's'), k.Type == KeyEnter:
			var ids []int64
			for _, st := range stats {
				if !in[st.Area.ID] {
					ids = append(ids, st.Area.ID)
				}
			}
			if err := s.Messages.SetUnsubscribedAreas(u.ID, ids); err != nil {
				return err
			}
			s.logInfo("%s changed their areas: %d of %d", u.Username, len(stats)-len(ids), len(stats))
			return s.scanNote(term, term.T("myareas.saved", "COUNT", len(stats)-len(ids), "TOTAL", len(stats)))
		case isKey(k, 'q'), k.Type == KeyEscape:
			return term.Print(ansi.ClearScreen() + ansi.Reset)
		}
	}
}

func newCount(term *Terminal, n int) string {
	if n == 0 {
		return ""
	}
	return term.T("common.count_new", "COUNT", n)
}
