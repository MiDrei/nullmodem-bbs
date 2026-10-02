package doors

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Daily maintenance: many classic doors (TradeWars, BRE, Usurper ...)
// expect a program run once a day -- new turns, the day's events, the
// news. A door with Daily set gets it here, headless: a DOS door in
// DOSBox-X with no caller attached, a native one as a plain process.
// Never while someone is playing it (that could corrupt its data):
// then it waits for the next check.

// DailyTimeout bounds one run.
var DailyTimeout = 10 * time.Minute

// ErrBusy: someone is playing the door; the run waits.
var ErrBusy = errors.New("doors: someone is playing it")

// dosboxDailyTemplate is dosboxConfigTemplate without a caller: no
// serial port, and the commands run from C: then exit.
const dosboxDailyTemplate = `[dosbox]
machine=svga_s3
memsize=4

[cpu]
core=auto
cputype=auto
cycles=max

[serial]
serial1=disabled

[dos]
xms=true
ems=true
ver=6.22

[autoexec]
MOUNT C %s
MOUNT D %s
C:
%s
EXIT
`

// RunDaily runs door's daily maintenance and returns what it printed
// (a native door's output; DOSBox-X's own log for a DOS door).
func RunDaily(ctx context.Context, door Door) (string, error) {
	if strings.TrimSpace(door.Daily) == "" {
		return "", errors.New("doors: no daily maintenance set")
	}
	activeMu.Lock()
	busy := active[door.Name] > 0
	if !busy {
		// Counted as a session while it runs: nobody starts the door
		// meanwhile -- they get the busy note from playing instead.
		active[door.Name]++
	}
	activeMu.Unlock()
	if busy {
		return "", ErrBusy
	}
	defer func() {
		activeMu.Lock()
		active[door.Name]--
		activeMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, DailyTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch door.Kind {
	case "dosbox":
		nodeDir, err := os.MkdirTemp("", "nullmodem-daily-*")
		if err != nil {
			return "", fmt.Errorf("doors: %w", err)
		}
		defer os.RemoveAll(nodeDir)
		dir, err := filepath.Abs(door.DOSBoxDir)
		if err != nil {
			return "", fmt.Errorf("doors: %w", err)
		}
		conf := fmt.Sprintf(dosboxDailyTemplate, dir, nodeDir, expandLaunchCmd(door.Daily, "DOOR.SYS", 1))
		confPath := filepath.Join(nodeDir, "dosbox.conf")
		if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
			return "", fmt.Errorf("doors: %w", err)
		}
		cmd = exec.CommandContext(ctx, "dosbox-x", "-conf", confPath)
		cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy")
	case "rlogin":
		return "", errors.New("doors: a remote door is maintained by its own system")
	default:
		fields := strings.Fields(door.Daily)
		exe := fields[0]
		if !filepath.IsAbs(exe) {
			exe = filepath.Join(door.Dir, exe)
		}
		if abs, err := filepath.Abs(exe); err == nil {
			exe = abs
		}
		cmd = exec.CommandContext(ctx, exe, fields[1:]...)
		cmd.Dir = door.Dir
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	stopWithParent(cmd)
	err := cmd.Run()
	text := out.String()
	if len(text) > 4000 {
		text = "…" + text[len(text)-4000:]
	}
	if ctx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("doors: %s's daily maintenance didn't finish within %s", door.Name, DailyTimeout)
	}
	if err != nil {
		return text, fmt.Errorf("doors: %s's daily maintenance: %w", door.Name, err)
	}
	return text, nil
}

// DailyState is a door's last daily maintenance run.
type DailyState struct {
	Door        string    `json:"door"`
	LastAt      time.Time `json:"last_at"`
	OK          bool      `json:"ok"`
	Detail      string    `json:"detail"`
	RequestedAt time.Time `json:"requested_at"`
}

// DailyStates returns the recorded runs, by door name.
func DailyStates(db *sql.DB) (map[string]DailyState, error) {
	rows, err := db.Query(`SELECT door, last_at, ok, detail, requested_at FROM door_daily`)
	if err != nil {
		return nil, fmt.Errorf("doors: %w", err)
	}
	defer rows.Close()
	out := map[string]DailyState{}
	for rows.Next() {
		var st DailyState
		var last, req int64
		if err := rows.Scan(&st.Door, &last, &st.OK, &st.Detail, &req); err != nil {
			return nil, fmt.Errorf("doors: %w", err)
		}
		if last > 0 {
			st.LastAt = time.UnixMilli(last)
		}
		if req > 0 {
			st.RequestedAt = time.UnixMilli(req)
		}
		out[st.Door] = st
	}
	return out, rows.Err()
}

// RequestDaily asks the bbs daemon to run door's maintenance now.
func RequestDaily(db *sql.DB, door string) error {
	_, err := db.Exec(`INSERT INTO door_daily (door, requested_at) VALUES (?, ?)
		ON CONFLICT(door) DO UPDATE SET requested_at = excluded.requested_at`, door, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("doors: %w", err)
	}
	return nil
}

// dailyAt parses "HH:MM" (default 00:05) for day.
func dailyAt(spec string, day time.Time) time.Time {
	h, m := 0, 5
	if spec != "" {
		if t, err := time.Parse("15:04", spec); err == nil {
			h, m = t.Hour(), t.Minute()
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, day.Location())
}

// dailyDue reports whether door's maintenance should run at now:
// asked for since its last run, or past today's time and not run today.
func dailyDue(door Door, st DailyState, now time.Time) bool {
	if !st.RequestedAt.IsZero() {
		return true // a run clears the request it served
	}
	if now.Before(dailyAt(door.DailyAt, now)) {
		return false
	}
	return st.LastAt.IsZero() || st.LastAt.Format("2006-01-02") != now.Format("2006-01-02") ||
		st.LastAt.Before(dailyAt(door.DailyAt, now))
}

// Scheduler runs the doors' daily maintenance when due; doors returns
// the current door list (changes in the web admin apply without a
// restart).
type Scheduler struct {
	DB     *sql.DB
	Doors  func() []Door
	Logger Logger
	// RunOne is RunDaily; tests replace it.
	RunOne func(context.Context, Door) (string, error)
	// Every defaults to 30 seconds.
	Every time.Duration
}

// Tick runs whatever is due now.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) {
	states, err := DailyStates(s.DB)
	if err != nil {
		s.Logger.Warn("%v", err)
		return
	}
	run := s.RunOne
	if run == nil {
		run = RunDaily
	}
	for _, d := range s.Doors() {
		if strings.TrimSpace(d.Daily) == "" || d.Kind == "rlogin" || !dailyDue(d, states[d.Name], now) {
			continue
		}
		started := time.Now()
		out, err := run(ctx, d)
		if errors.Is(err, ErrBusy) {
			continue // next time, once they're out
		}
		detail := strings.TrimSpace(out)
		if err != nil {
			detail = err.Error() + suffixOutput(detail)
			s.Logger.Warn("%s: daily maintenance failed: %v", d.Name, err)
		} else {
			s.Logger.Info("%s: daily maintenance done in %s", d.Name, time.Since(started).Round(time.Second))
		}
		// A request made while this ran stays for the next tick.
		if _, dbErr := s.DB.Exec(`INSERT INTO door_daily (door, last_day, last_at, ok, detail) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(door) DO UPDATE SET last_day = excluded.last_day, last_at = excluded.last_at, ok = excluded.ok, detail = excluded.detail,
				requested_at = CASE WHEN requested_at <= ? THEN 0 ELSE requested_at END`,
			d.Name, now.Format("2006-01-02"), time.Now().UnixMilli(), err == nil, detail, started.UnixMilli()); dbErr != nil {
			s.Logger.Warn("%v", dbErr)
		}
	}
}

func suffixOutput(out string) string {
	if out == "" {
		return ""
	}
	return "\n" + out
}

// Run checks every Every until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	every := s.Every
	if every == 0 {
		every = 30 * time.Second
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.Tick(ctx, time.Now())
	}
}
