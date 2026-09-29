package doors

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY opens a new pseudo-terminal pair, the slave sized 80x25 --
// for driving a door's full-screen console setup program (see
// scriptConsole).
func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("opening /dev/ptmx: %w", err)
	}
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("unlocking pty: %w", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("getting pty number: %w", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("opening pty slave: %w", err)
	}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 25, Col: 80}); err != nil {
		master.Close()
		slave.Close()
		return nil, nil, fmt.Errorf("sizing pty: %w", err)
	}
	return master, slave, nil
}

// attachPTY makes slave cmd's controlling terminal and its stdio.
func attachPTY(cmd *exec.Cmd, slave *os.File) {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
}

// stopWithParent has the kernel end cmd when this process dies, so a
// door's background program never outlives the bbs daemon (and ends
// up connected twice after its restart). Call after attachPTY.
func stopWithParent(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}

// terminate ends cmd's process group: SIGTERM, then SIGKILL if it
// hasn't exited after killGrace.
func terminate(cmd *exec.Cmd, exited <-chan error) {
	pid := cmd.Process.Pid
	syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(killGrace):
		syscall.Kill(-pid, syscall.SIGKILL)
		<-exited
	}
}
