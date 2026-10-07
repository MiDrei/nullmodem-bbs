package doors

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// consoleStep waits for Expect to appear on a console program's screen
// (compared with ANSI escapes and all whitespace removed), then types
// Send.
type consoleStep struct {
	Expect string
	Send   string
}

var (
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	spaces     = regexp.MustCompile(`\s+`)
)

// scriptConsole runs exe (in dir) on a pseudo-terminal and walks it
// through steps, like a sysop would at the keyboard -- for doors whose
// first-time setup is only offered in a full-screen console program
// (Usurper's EDITOR "Reset Game"). After the last step it gives the
// program a moment to exit on its own, then kills it; the caller
// checks the result on disk.
func scriptConsole(ctx context.Context, dir, exe string, steps []consoleStep) error {
	master, slave, err := openPTY()
	if err != nil {
		return err
	}
	defer master.Close()

	cmd := exec.CommandContext(ctx, exe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm")
	attachPTY(cmd, slave)
	if err := cmd.Start(); err != nil {
		slave.Close()
		return fmt.Errorf("starting %s: %w", exe, err)
	}
	slave.Close()
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}()

	var (
		mu  sync.Mutex
		out bytes.Buffer
	)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	screen := func() string {
		mu.Lock()
		defer mu.Unlock()
		// Latin-1 decoding keeps every byte; only the ASCII text the
		// steps look for matters.
		// Compared without escapes and without any whitespace: a
		// highlighted hotkey letter ("R" in "Reset") has an escape right
		// inside its word, and cursor moves stand in for spaces.
		return spaces.ReplaceAllString(ansiEscape.ReplaceAllString(out.String(), ""), "")
	}

	for _, st := range steps {
		want := spaces.ReplaceAllString(st.Expect, "")
		for !strings.Contains(screen(), want) {
			select {
			case <-ctx.Done():
				return fmt.Errorf("waiting for %q: %w", st.Expect, ctx.Err())
			case <-done:
				return fmt.Errorf("%s exited while waiting for %q", exe, st.Expect)
			case <-time.After(200 * time.Millisecond):
			}
		}
		mu.Lock()
		out.Reset() // the next step waits for new output only
		mu.Unlock()
		time.Sleep(300 * time.Millisecond) // let a dialog finish drawing before typing
		if _, err := master.WriteString(st.Send); err != nil {
			return fmt.Errorf("typing into %s: %w", exe, err)
		}
	}
	return nil
}
