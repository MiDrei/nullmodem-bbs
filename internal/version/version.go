// Package version holds the single canonical NullModem BBS version
// string, shared by the bbs and web daemons (two separate binaries)
// so they can't drift out of sync.
package version

import "strings"

// Version is shown on the BBS welcome screen, the [V]ersion menu
// command, and the web admin dashboard.
const Version = "NullModem BBS v0.67.0"

// Short is just the version number (e.g. "0.24.0-dev"), without the
// "NullModem BBS v" prefix -- for contexts where that prefix is
// redundant or unwanted, e.g. BinkP's M_NUL "VER ..." identification
// line (see internal/binkp), which already supplies its own product
// name ahead of the version.
func Short() string {
	return strings.TrimPrefix(Version, "NullModem BBS v")
}
