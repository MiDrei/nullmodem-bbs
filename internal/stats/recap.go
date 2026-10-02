package stats

import (
	"fmt"
	"sort"
	"strings"
)

// RecapExtra is what the recap reports beyond the statistics: what
// needs attention now, the newest backup, accounts waiting.
type RecapExtra struct {
	BBSName  string
	Version  string
	Problems []string
	Backup   string // "" when backups are off
	Waiting  int    // accounts waiting for approval
}

// Recap is the monthly recap netmail: the last days days in plain
// text, at most 79 columns -- read on any terminal.
func (s *Store) Recap(title string, days int, x RecapExtra) (string, error) {
	r, err := s.Report(days, true)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	line := func(label, format string, args ...any) {
		fmt.Fprintf(&b, "  %-15s %s\n", label, fmt.Sprintf(format, args...))
	}
	list := func(items []Ranked, unit string, n int) string {
		var parts []string
		for i, it := range items {
			if i == n {
				break
			}
			p := fmt.Sprintf("%s %d%s", it.Name, it.Count, unit)
			if it.Minutes > 0 {
				p += fmt.Sprintf(" (%d min)", it.Minutes)
			}
			parts = append(parts, p)
		}
		if len(parts) == 0 {
			return "--"
		}
		return wrap(strings.Join(parts, ", "), 61, 18)
	}

	fmt.Fprintf(&b, "%s -- %s\n%s\n\n", title, x.BBSName, strings.Repeat("=", min(79, len(title)+len(x.BBSName)+4)))

	b.WriteString("Callers\n")
	via := make([]string, 0, len(r.Via))
	for _, v := range r.Via {
		via = append(via, fmt.Sprintf("%s %d", v.Name, v.Count))
	}
	calls := fmt.Sprintf("%d by %d caller(s)", r.Calls, r.Callers)
	if len(via) > 0 {
		calls += " (" + strings.Join(via, ", ") + ")"
	}
	line("Calls", "%s", calls)
	busiest := Day{}
	for _, d := range r.CallsPerDay {
		if d.Count > busiest.Count {
			busiest = d
		}
	}
	if busiest.Count > 0 {
		line("Busiest day", "%s (%d calls)", busiest.Date, busiest.Count)
	}
	line("Called most", "%s", list(r.TopCallers, "", 5))
	newUsers := 0
	if len(r.NewUsers) > 0 {
		for _, n := range r.NewUsers {
			newUsers += n.Count
		}
	}
	accounts := fmt.Sprintf("%d new in the last 12 months", newUsers)
	if x.Waiting > 0 {
		accounts += fmt.Sprintf(", %d waiting for approval", x.Waiting)
	}
	line("Accounts", "%s", accounts)

	b.WriteString("\nMessages\n")
	line("Written here", "%d", r.Posts)
	line("Writers", "%s", list(r.TopPosters, "", 5))
	line("Busiest areas", "%s", list(r.TopAreas, "", 5))
	if len(r.Networks) > 0 {
		b.WriteString("\nEchomail by network (last 12 weeks: received / written here)\n")
		nets := append([]NetworkTraffic(nil), r.Networks...)
		sort.Slice(nets, func(i, j int) bool { return nets[i].In+nets[i].Out > nets[j].In+nets[j].Out })
		for _, n := range nets {
			line(n.Network, "%6d / %d", n.In, n.Out)
		}
	}

	b.WriteString("\nThe rest\n")
	line("Doors", "%s", list(r.TopDoors, "x", 5))
	line("Downloads", "%s", list(r.TopFiles, "x", 5))
	var sessions []Ranked
	for _, u := range r.Uplinks {
		u.Name = fmt.Sprintf("%s %d", u.Name, u.Count)
		if u.Detail != "" && u.Detail != "0" {
			u.Name += " (" + u.Detail + " failed)"
		}
		sessions = append(sessions, u)
	}
	var names []string
	for _, u := range sessions {
		names = append(names, u.Name)
	}
	if len(names) == 0 {
		names = []string{"--"}
	}
	line("BinkP sessions", "%s", wrap(strings.Join(names, ", "), 61, 18))
	if x.Backup != "" {
		line("Newest backup", "%s", x.Backup)
	}

	b.WriteString("\nRight now\n")
	if len(x.Problems) == 0 {
		b.WriteString("  Nothing needs attention.\n")
	}
	for _, p := range x.Problems {
		b.WriteString("  - " + wrap(p, 75, 4) + "\n")
	}
	fmt.Fprintf(&b, "\n-- NullModem BBS %s\n", x.Version)
	return b.String(), nil
}

// wrap breaks text at width, continuing lines indented by indent.
func wrap(text string, width, indent int) string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(text) {
		if cur != "" && len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		if cur != "" {
			cur += " "
		}
		cur += w
	}
	lines = append(lines, cur)
	return strings.Join(lines, "\n"+strings.Repeat(" ", indent))
}
