package user

import "strings"

// reservedUsernames and reservedRealNames block impersonation of a
// system/staff role at registration -- ported directly from
// BinktermPHP's UserRestrictions (src/UserRestrictions.php), the BBS
// software this system replaced, rather than inventing a fresh list.
// The two lists differ slightly (BinktermPHP's own choice, kept as-is
// for fidelity): a username additionally blocks a handful of
// address-like handles ("postmaster", "webmaster", ...) a real name
// wouldn't plausibly be typed as, while a real name additionally
// blocks spelled-out role titles ("system operator", ...) nobody
// would choose as a one-word handle.
var reservedUsernames = map[string]bool{
	"system": true, "root": true, "sysop": true, "admin": true,
	"administrator": true, "sysadmin": true, "sysadm": true,
	"moderator": true, "mod": true, "staff": true, "support": true,
	"helpdesk": true, "postmaster": true, "webmaster": true,
	"nobody": true, "anonymous": true, "guest": true,
}

var reservedRealNames = map[string]bool{
	"system": true, "root": true, "sysop": true,
	"system operator": true, "system administrator": true,
	"admin": true, "administrator": true, "sysadmin": true, "sysadm": true,
	"moderator": true, "staff": true, "support": true,
	"anonymous": true, "guest": true,
}

// IsRestrictedUsername reports whether username (matched case-
// insensitively after trimming) is a reserved system/staff role no
// new account may register as. An existing account already using one
// (registered before this existed) is unaffected -- only checked at
// registration time, never at login.
func IsRestrictedUsername(username string) bool {
	return reservedUsernames[strings.ToLower(strings.TrimSpace(username))]
}

// IsRestrictedRealName is IsRestrictedUsername's counterpart for the
// optional real-name field.
func IsRestrictedRealName(realName string) bool {
	return reservedRealNames[strings.ToLower(strings.TrimSpace(realName))]
}
