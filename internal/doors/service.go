package doors

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/services"
)

// Program is a door's background program: a process that must run all
// the time for the door to work, like uMRC's umrc-bridge, which keeps
// the connection to the chat network the door's sessions talk through.
type Program struct {
	// Door is the door's name; the program is listed as
	// services.DoorPrefix+Door on the admin's Services page.
	Door string
	// Dir is the working directory, the door's own.
	Dir string
	// Command is the program and its arguments; a relative program
	// path is taken relative to Dir.
	Command []string
}

// ServiceName is how the program appears in the services table.
func (p Program) ServiceName() string { return services.DoorPrefix + p.Door }

func (p Program) key() string {
	return p.Door + "\x00" + p.Dir + "\x00" + strings.Join(p.Command, "\x00")
}

// Logger is what Supervisor logs to (internal/applog.Logger).
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
}

// Supervisor keeps the doors' background programs running: it starts
// each one Programs lists, restarts it when it exits (with growing
// pauses if it keeps failing) or when a restart is asked for in the
// web admin, and stops it once its door no longer lists it. Their
// output goes to the log.
type Supervisor struct {
	// Programs returns the programs that should run; it is called
	// again every CheckEvery, so doors changed in the web admin take
	// effect without restarting the bbs daemon.
	Programs func() []Program
	// Store, if set, lists the programs on the Services page and
	// passes restart requests on.
	Store  *services.Store
	Logger Logger
	// CheckEvery defaults to 10 seconds.
	CheckEvery time.Duration
}

// Backoff after a program exits: MinBackoff, doubling up to MaxBackoff
// while it keeps exiting within StableAfter of being started.
var (
	MinBackoff  = 5 * time.Second
	MaxBackoff  = 5 * time.Minute
	StableAfter = time.Minute
	killGrace   = 5 * time.Second
)

// Run supervises until ctx ends, then stops every program.
func (s *Supervisor) Run(ctx context.Context) {
	every := s.CheckEvery
	if every == 0 {
		every = 10 * time.Second
	}
	type running struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	active := map[string]running{}
	stop := func(k string) {
		r := active[k]
		r.cancel()
		<-r.done
		delete(active, k)
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		want := map[string]Program{}
		var names []string
		for _, p := range s.Programs() {
			if len(p.Command) == 0 {
				continue
			}
			want[p.key()] = p
			names = append(names, p.ServiceName())
		}
		for k := range active {
			if _, ok := want[k]; !ok {
				stop(k)
			}
		}
		for k, p := range want {
			if _, ok := active[k]; ok {
				continue
			}
			pctx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			active[k] = running{cancel, done}
			go func() {
				defer close(done)
				s.supervise(pctx, p)
			}()
		}
		if s.Store != nil {
			if err := s.Store.RemoveDoorPrograms(names); err != nil {
				s.Logger.Warn("%v", err)
			}
		}
		select {
		case <-ctx.Done():
			for k := range active {
				stop(k)
			}
			return
		case <-tick.C:
		}
	}
}

// supervise runs p again and again until ctx ends.
func (s *Supervisor) supervise(ctx context.Context, p Program) {
	backoff := MinBackoff
	for {
		started := time.Now()
		restarted := s.runOnce(ctx, p)
		if ctx.Err() != nil {
			return
		}
		if restarted {
			backoff = MinBackoff
			continue
		}
		if time.Since(started) >= StableAfter {
			backoff = MinBackoff
		}
		s.Logger.Warn("%s: background program not running, starting it again in %s", p.Door, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, MaxBackoff)
	}
}

// runOnce starts p and waits for it to exit, stopping it when ctx ends
// or a restart is asked for (reported as restarted).
func (s *Supervisor) runOnce(ctx context.Context, p Program) (restarted bool) {
	exe := p.Command[0]
	if !filepath.IsAbs(exe) {
		exe = filepath.Join(p.Dir, exe)
	}
	// Absolute: exec.Cmd resolves a relative program path against Dir,
	// which would double a relative door directory.
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	cmd := exec.Command(exe, p.Command[1:]...)
	cmd.Dir = p.Dir
	// On a terminal: C programs buffer their output fully when it's a
	// pipe, and it would only reach the log once they exit.
	master, slave, err := openPTY()
	if err != nil {
		s.Logger.Warn("%s: background program: %v", p.Door, err)
		return false
	}
	defer master.Close()
	attachPTY(cmd, slave)
	stopWithParent(cmd)
	err = cmd.Start()
	slave.Close()
	if err != nil {
		s.Logger.Warn("%s: starting background program %s: %v", p.Door, p.Command[0], err)
		return false
	}
	s.Logger.Info("%s: background program %s started (pid %d)", p.Door, filepath.Base(exe), cmd.Process.Pid)

	var startedAt int64
	if s.Store != nil {
		if startedAt, err = s.Store.RegisterProcess(p.ServiceName(), filepath.Base(exe), cmd.Process.Pid); err != nil {
			s.Logger.Warn("%v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(master)
		sc.Buffer(make([]byte, 4096), 64<<10)
		sc.Split(scanTerminalLines)
		for sc.Scan() {
			if line := cleanTerminalLine(sc.Text()); line != "" {
				s.Logger.Info("%s: %s", p.Door, line)
			}
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	beat := time.NewTicker(2 * time.Second)
	defer beat.Stop()
	for {
		select {
		case err := <-exited:
			// The output reader ends by itself: EIO once the program
			// (and anything it started) has closed the terminal.
			master.SetReadDeadline(time.Now().Add(time.Second))
			wg.Wait()
			if err != nil {
				s.Logger.Warn("%s: background program exited: %v", p.Door, err)
			}
			return false
		case <-ctx.Done():
			terminate(cmd, exited)
			s.Logger.Info("%s: background program stopped", p.Door)
			return false
		case <-beat.C:
			if s.Store == nil {
				continue
			}
			s.Store.Heartbeat(p.ServiceName())
			if s.Store.RestartRequested(p.ServiceName(), startedAt) {
				s.Logger.Info("%s: restarting background program, as asked in the web admin", p.Door)
				terminate(cmd, exited)
				return true
			}
		}
	}
}

// scanTerminalLines splits at CR or LF: console programs redraw a
// status line with a bare CR.
func scanTerminalLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// cleanTerminalLine drops ANSI escapes and control characters.
func cleanTerminalLine(s string) string {
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
