// Package version holds the single canonical NullModem BBS version
// string, shared by the bbs and web daemons (two separate binaries)
// so they can't drift out of sync, and the build it came from.
package version

import (
	"runtime/debug"
	"strings"
	"time"
)

// Version is shown on the BBS welcome screen, the [V]ersion menu
// command, and the web admin dashboard.
const Version = "NullModem BBS v0.95.2"

// Commit (short hash) and BuildDate (RFC 3339) are set when building
// the image: go build -ldflags "-X .../version.Commit=... -X
// .../version.BuildDate=...". A plain go build in a checkout takes
// them from the Go toolchain's VCS stamp instead.
var (
	Commit    string
	BuildDate string
)

// Short is just the version number (e.g. "0.24.0-dev"), without the
// "NullModem BBS v" prefix -- for contexts where that prefix is
// redundant or unwanted, e.g. BinkP's M_NUL "VER ..." identification
// line (see internal/binkp), which already supplies its own product
// name ahead of the version.
func Short() string {
	return strings.TrimPrefix(Version, "NullModem BBS v")
}

// commitAndTime is the build's commit and time, from the link flags or
// else the VCS stamp; "" and zero when unknown.
func commitAndTime() (string, time.Time) {
	commit := Commit
	built, _ := time.Parse(time.RFC3339, BuildDate)
	if commit != "" {
		return commit, built
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", built
	}
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value[:min(7, len(s.Value))]
		case "vcs.time":
			if built.IsZero() {
				built, _ = time.Parse(time.RFC3339, s.Value)
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if commit != "" && modified {
		commit += "-dirty"
	}
	return commit, built
}

// Build says which build this is -- "a1cf7b5, 2026-10-07 13:54 UTC" --
// or "" when it isn't known.
func Build() string {
	commit, built := commitAndTime()
	var parts []string
	if commit != "" {
		parts = append(parts, commit)
	}
	if !built.IsZero() {
		parts = append(parts, built.UTC().Format("2006-01-02 15:04 MST"))
	}
	return strings.Join(parts, ", ")
}

// CommitShort is the build's commit, "" when unknown.
func CommitShort() string {
	commit, _ := commitAndTime()
	return commit
}

// Full is Version with the build: "NullModem BBS v0.90.8 (a1cf7b5,
// 2026-10-07 13:54 UTC)".
func Full() string {
	if b := Build(); b != "" {
		return Version + " (" + b + ")"
	}
	return Version
}

// ShortBuild is Short with the commit: "0.90.8 (a1cf7b5)" -- what each
// daemon registers on the Services page.
func ShortBuild() string {
	if c := CommitShort(); c != "" {
		return Short() + " (" + c + ")"
	}
	return Short()
}
