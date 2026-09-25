//go:build unix && !linux

package doors

import "syscall"

// socketpair returns a connected AF_UNIX stream pair with both ends
// close-on-exec. macOS and the BSDs have no SOCK_CLOEXEC for
// socketpair(2), so the flag is set right after creation instead --
// under syscall.ForkLock, which os/exec also holds while forking, so
// no concurrent exec elsewhere in this process can inherit the pair in
// between (the same pattern the standard library's net package uses on
// these systems).
func socketpair() ([2]int, error) {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return fds, err
	}
	syscall.CloseOnExec(fds[0])
	syscall.CloseOnExec(fds[1])
	return fds, nil
}
