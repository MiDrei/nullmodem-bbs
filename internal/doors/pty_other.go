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
