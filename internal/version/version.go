// Package version holds the single canonical NullModem BBS version
// string, shared by the bbs and web daemons (two separate binaries)
// so they can't drift out of sync.
package version

// Version is shown on the BBS welcome screen, the [V]ersion menu
// command, and the web admin dashboard.
const Version = "NullModem BBS v0.19.3-dev"
