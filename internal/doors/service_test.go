//go:build linux

package doors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *memLogger) Info(f string, a ...any) { l.add(f, a...) }
func (l *memLogger) Warn(f string, a ...any) { l.add(f, a...) }
func (l *memLogger) add(f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
}
func (l *memLogger) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSupervisorRestartsLogsAndStopsProgram: a program that prints a
// line without a newline and exits is logged (its output reaches the
// log although it's a C-style buffered console program's), started
// again after exiting, and stopped once no door lists it.
func TestSupervisorRestartsLogsAndStopsProgram(t *testing.T) {
	oldMin := MinBackoff
	MinBackoff = 50 * time.Millisecond
	defer func() { MinBackoff = oldMin }()

	// A relative door directory, as in the config: the program path
	// must not be resolved against it twice.
	base := t.TempDir()
	t.Chdir(base)
	os.Mkdir("door", 0o755)
	dir := "door"
	script := filepath.Join(dir, "bridge.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf 'Ready for clients\\r'\necho started >> runs\nsleep 0.1\n"), 0o755)

	var mu sync.Mutex
	programs := []Program{{Door: "Chat", Dir: dir, Command: []string{"bridge.sh"}}}
	log := &memLogger{}
	sup := &Supervisor{
		Programs: func() []Program {
			mu.Lock()
			defer mu.Unlock()
			return programs
		},
		Logger:     log,
		CheckEvery: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sup.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	runs := func() int {
		b, _ := os.ReadFile(filepath.Join(dir, "runs"))
		return strings.Count(string(b), "started")
	}
	waitFor(t, "a restart after the program exited", func() bool { return runs() >= 2 })
	waitFor(t, "its output in the log", func() bool { return log.count("Chat: Ready for clients") > 0 })

	// Replaced by a long-running one, then removed: it must be stopped.
	os.WriteFile(filepath.Join(dir, "long.sh"), []byte("#!/bin/sh\necho $$ > pid\nexec sleep 60\n"), 0o755)
	mu.Lock()
	programs = []Program{{Door: "Chat", Dir: dir, Command: []string{"long.sh"}}}
	mu.Unlock()
	var pid int
	waitFor(t, "the long-running program", func() bool {
		b, err := os.ReadFile(filepath.Join(dir, "pid"))
		return err == nil && func() bool { _, e := fmt.Sscan(string(b), &pid); return e == nil }()
	})
	stopped := log.count("Chat: background program stopped") // bridge.sh's, when replaced
	mu.Lock()
	programs = nil
	mu.Unlock()
	waitFor(t, "the program to be stopped", func() bool { return log.count("Chat: background program stopped") > stopped })
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if !strings.Contains(string(b), ") Z ") {
			t.Fatalf("process %d still running after its door was removed", pid)
		}
	}
}
