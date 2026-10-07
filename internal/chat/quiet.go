package chat

import "github.com/midrei/nullmodem-bbs/internal/user"

// QuietSysops is a Store.Quiet for the sysops (security level 255),
// unless announce says they are announced.
func QuietSysops(users *user.Store, announce func() bool) func(string) bool {
	return func(username string) bool {
		if announce() {
			return false
		}
		u, err := users.ByUsername(username)
		return err == nil && u.SecurityLevel >= user.SLSysop
	}
}
