package doors

import "syscall"

// socketpair returns a connected AF_UNIX stream pair with both ends
// close-on-exec, set atomically at creation via SOCK_CLOEXEC.
func socketpair() ([2]int, error) {
	return syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
}
