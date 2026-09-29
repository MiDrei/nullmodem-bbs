//go:build !linux

package doors

import (
	"errors"
	"os"
	"os/exec"
)

func openPTY() (master, slave *os.File, err error) {
	return nil, nil, errors.New("scripting a console program needs Linux")
}

func attachPTY(cmd *exec.Cmd, slave *os.File) {}

func stopWithParent(cmd *exec.Cmd) {}

func terminate(cmd *exec.Cmd, exited <-chan error) {
	cmd.Process.Kill()
	<-exited
}
