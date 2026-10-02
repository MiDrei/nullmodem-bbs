// Package stats records calls and door sessions and sums up what goes
// on at the board: calls per day, the most active callers, posters and
// areas, echomail traffic per network, doors and downloads. Report
// returns the public part (the front page) or, for the sysop, all of it.
package stats

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

// Store records into and reports from the shared database. A nil
// *Store records nothing (tests, tools).
type Store struct {
	db *sql.DB
}

func NewStore(sqlDB *sql.DB) *Store { return &Store{db: sqlDB} }

// RecordCall logs a login: via is "telnet", "ssh" or "web". Web logins
// of the same caller within db.WebCallGap count once.
func (s *Store) RecordCall(userID int64, via string) error {
	if s == nil {
		return nil
	}
	if via == "web" {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM calls WHERE user_id = ? AND via = 'web' AND at >= datetime('now', ?)`,
			userID, fmt.Sprintf("-%d seconds", int(db.WebCallGap.Seconds()))).Scan(&n); err == nil && n > 0 {
			return nil
		}
	}
	if _, err := s.db.Exec(`INSERT INTO calls (user_id, via) VALUES (?, ?)`, userID, via); err != nil {
		return fmt.Errorf("stats: record call: %w", err)
	}
	return nil
}

// RecordDoor logs a door session that just ended after d.
func (s *Store) RecordDoor(door string, userID int64, d time.Duration) error {
	if s == nil {
		return nil
	}
	secs := int(d.Seconds())
	if _, err := s.db.Exec(`INSERT INTO door_sessions (door, user_id, started_at, seconds) VALUES (?, ?, datetime('now', ?), ?)`,
		door, userID, fmt.Sprintf("-%d seconds", secs), secs); err != nil {
		return fmt.Errorf("stats: record door: %w", err)
	}
	return nil
}

// Day is one day's number.
type Day struct {
	Date  string `json:"date"` // YYYY-MM-DD, server time
	Count int    `json:"count"`
}

// Ranked is a name and its number (and, for doors, minutes played).
type Ranked struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Detail  string `json:"detail,omitempty"`
	Minutes int    `json:"minutes,omitempty"`
}

// Week is a network's echomail in one week: received and sent from here.
type Week struct {
	Week string `json:"week"` // the Monday, YYYY-MM-DD
	In   int    `json:"in"`
	Out  int    `json:"out"`
}

// NetworkTraffic is a network's weekly echomail.
type NetworkTraffic struct {
	Network string `json:"network"`
	In      int    `json:"in"`
	Out     int    `json:"out"`
	Weeks   []Week `json:"weeks"`
}

// Report: the last Days days (traffic: Weeks weeks).
type Report struct {
	Days        int              `json:"days"`
	Calls       int              `json:"calls"`
	Callers     int              `json:"callers"`
	Posts       int              `json:"posts"`
	CallsPerDay []Day            `json:"calls_per_day"`
	TopCallers  []Ranked         `json:"top_callers"`
	TopPosters  []Ranked         `json:"top_posters"`
	TopAreas    []Ranked         `json:"top_areas"`
	Networks    []NetworkTraffic `json:"networks"`
	TopDoors    []Ranked         `json:"top_doors"`
	TopFiles    []Ranked         `json:"top_files"`

	// Sysop only.
	Via         []Ranked `json:"via,omitempty"`
	ByHour      []int    `json:"by_hour,omitempty"` // calls per hour of day, server time
	NewUsers    []Ranked `json:"new_users,omitempty"`
	Uplinks     []Ranked `json:"uplinks,omitempty"` // BinkP sessions per peer; Detail: failures
	PostsPerDay []Day    `json:"posts_per_day,omitempty"`
}

const weeks = 12

// Report sums up the last days days; full adds the sysop's part.
func (s *Store) Report(days int, full bool) (*Report, error) {
	r := &Report{Days: days}
	since := fmt.Sprintf("-%d days", days)
	q := func(dest func(*sql.Rows) error, query string, args ...any) error {
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return fmt.Errorf("stats: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := dest(rows); err != nil {
				return fmt.Errorf("stats: %w", err)
			}
		}
		return rows.Err()
	}
	ranked := func(into *[]Ranked) func(*sql.Rows) error {
		return func(rows *sql.Rows) error {
			var x Ranked
			var detail sql.NullString
			if err := rows.Scan(&x.Name, &x.Count, &detail); err != nil {
				return err
			}
			x.Name, x.Detail = cp437(x.Name), cp437(detail.String)
			*into = append(*into, x)
			return nil
		}
	}

	// Calls: per day (server time), totals, the busiest callers.
	perDay := map[string]int{}
	callers := map[int64]bool{}
	byHour := make([]int, 24)
	if err := q(func(rows *sql.Rows) error {
		var at string
		var user int64
		if err := rows.Scan(&at, &user); err != nil {
			return err
		}
		t, err := time.ParseInLocation("2006-01-02 15:04:05", at, time.UTC)
		if err != nil {
			return nil
		}
		t = t.Local()
		perDay[t.Format("2006-01-02")]++
		byHour[t.Hour()]++
		callers[user] = true
		r.Calls++
		return nil
	}, `SELECT substr(at, 1, 19), user_id FROM calls WHERE at >= datetime('now', ?)`, since); err != nil {
		return nil, err
	}
	r.Callers = len(callers)
	r.CallsPerDay = lastDays(days, perDay)
	if err := q(ranked(&r.TopCallers), `SELECT u.username, COUNT(*), u.location FROM calls c JOIN users u ON u.id = c.user_id
		WHERE c.at >= datetime('now', ?) GROUP BY c.user_id ORDER BY COUNT(*) DESC, u.username LIMIT 5`, since); err != nil {
		return nil, err
	}

	// Posts written here, and the areas with the most mail (shown ones).
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE from_user_id IS NOT NULL AND posted_at >= datetime('now', ?)`, since).Scan(&r.Posts); err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	if err := q(ranked(&r.TopPosters), `SELECT COALESCE(NULLIF(m.from_name, ''), u.username), COUNT(*), NULL FROM messages m JOIN users u ON u.id = m.from_user_id
		WHERE m.posted_at >= datetime('now', ?) GROUP BY m.from_user_id ORDER BY COUNT(*) DESC LIMIT 5`, since); err != nil {
		return nil, err
	}
	if err := q(ranked(&r.TopAreas), `SELECT a.name, COUNT(*), a.network FROM messages m JOIN message_areas a ON a.id = m.area_id
		WHERE a.hidden = 0 AND a.pending = 0 AND m.posted_at >= datetime('now', ?)
		GROUP BY a.id ORDER BY COUNT(*) DESC LIMIT 5`, since); err != nil {
		return nil, err
	}

	// Echomail per network and week: received (remote) and sent (local).
	nets := map[string]*NetworkTraffic{}
	if err := q(func(rows *sql.Rows) error {
		var network, day string
		var local bool
		var n int
		if err := rows.Scan(&network, &day, &local, &n); err != nil {
			return err
		}
		t, err := time.Parse("2006-01-02", day)
		if err != nil {
			return nil
		}
		nt := nets[network]
		if nt == nil {
			nt = &NetworkTraffic{Network: network}
			nets[network] = nt
		}
		w := monday(t).Format("2006-01-02")
		i := sort.Search(len(nt.Weeks), func(i int) bool { return nt.Weeks[i].Week >= w })
		if i == len(nt.Weeks) || nt.Weeks[i].Week != w {
			nt.Weeks = append(nt.Weeks[:i], append([]Week{{Week: w}}, nt.Weeks[i:]...)...)
		}
		if local {
			nt.Weeks[i].Out += n
			nt.Out += n
		} else {
			nt.Weeks[i].In += n
			nt.In += n
		}
		return nil
	}, `SELECT a.network, substr(m.posted_at, 1, 10), m.from_user_id IS NOT NULL, COUNT(*)
		FROM messages m JOIN message_areas a ON a.id = m.area_id
		WHERE a.network != '' AND m.posted_at >= datetime('now', ?)
		GROUP BY a.network, substr(m.posted_at, 1, 10), m.from_user_id IS NOT NULL`, fmt.Sprintf("-%d days", weeks*7)); err != nil {
		return nil, err
	}
	for _, nt := range nets {
		nt.Weeks = fillWeeks(nt.Weeks)
		r.Networks = append(r.Networks, *nt)
	}
	sort.Slice(r.Networks, func(i, j int) bool {
		a, b := r.Networks[i], r.Networks[j]
		if a.In+a.Out != b.In+b.Out {
			return a.In+a.Out > b.In+b.Out
		}
		return strings.ToLower(a.Network) < strings.ToLower(b.Network)
	})

	// Doors by sessions (with minutes), downloads of all time.
	if err := q(func(rows *sql.Rows) error {
		var x Ranked
		var secs int
		if err := rows.Scan(&x.Name, &x.Count, &secs); err != nil {
			return err
		}
		x.Minutes = (secs + 30) / 60
		r.TopDoors = append(r.TopDoors, x)
		return nil
	}, `SELECT door, COUNT(*), COALESCE(SUM(seconds), 0) FROM door_sessions WHERE started_at >= datetime('now', ?)
		GROUP BY door ORDER BY COUNT(*) DESC LIMIT 5`, since); err != nil {
		return nil, err
	}
	if err := q(ranked(&r.TopFiles), `SELECT f.filename, f.download_count, a.name FROM files f JOIN file_areas a ON a.id = f.area_id
		WHERE f.download_count > 0 AND a.pending = 0 ORDER BY f.download_count DESC, f.filename LIMIT 5`); err != nil {
		return nil, err
	}

	if !full {
		return r, nil
	}
	r.ByHour = byHour
	if err := q(ranked(&r.Via), `SELECT via, COUNT(*), NULL FROM calls WHERE at >= datetime('now', ?) GROUP BY via ORDER BY COUNT(*) DESC`, since); err != nil {
		return nil, err
	}
	if err := q(ranked(&r.NewUsers), `SELECT substr(created_at, 1, 7), COUNT(*), NULL FROM users
		WHERE created_at >= datetime('now', '-12 months') GROUP BY substr(created_at, 1, 7) ORDER BY 1`); err != nil {
		return nil, err
	}
	if err := q(ranked(&r.Uplinks), `SELECT CASE WHEN peer_address != '' THEN peer_address ELSE peer_host END, COUNT(*),
			CAST(SUM(outcome != 'ok') AS TEXT)
		FROM binkp_sessions WHERE started_at >= datetime('now', ?) GROUP BY 1 ORDER BY COUNT(*) DESC LIMIT 12`, since); err != nil {
		return nil, err
	}
	posts := map[string]int{}
	if err := q(func(rows *sql.Rows) error {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return err
		}
		posts[day] = n
		return nil
	}, `SELECT substr(posted_at, 1, 10), COUNT(*) FROM messages WHERE posted_at >= datetime('now', ?) GROUP BY 1`, since); err != nil {
		return nil, err
	}
	r.PostsPerDay = lastDays(days, posts)
	return r, nil
}

// lastDays is days days up to today, each with its count (0 if none).
func lastDays(days int, counts map[string]int) []Day {
	out := make([]Day, 0, days)
	today := time.Now()
	for i := days - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, Day{Date: d, Count: counts[d]})
	}
	return out
}

func monday(t time.Time) time.Time {
	return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
}

// fillWeeks: all of the last weeks weeks, oldest first, gaps as zeros.
func fillWeeks(have []Week) []Week {
	byWeek := map[string]Week{}
	for _, w := range have {
		byWeek[w.Week] = w
	}
	start := monday(time.Now().UTC()).AddDate(0, 0, -7*(weeks-1))
	out := make([]Week, 0, weeks)
	for i := 0; i < weeks; i++ {
		k := start.AddDate(0, 0, 7*i).Format("2006-01-02")
		w := byWeek[k]
		w.Week = k
		out = append(out, w)
	}
	return out
}

// cp437: names and subjects are stored as CP437.
func cp437(s string) string { return ansi.DecodeCP437([]byte(s)) }
