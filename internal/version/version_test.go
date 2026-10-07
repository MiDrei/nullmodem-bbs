package version

import (
	"strings"
	"testing"
)

func TestBuildFromLinkFlags(t *testing.T) {
	oldC, oldD := Commit, BuildDate
	defer func() { Commit, BuildDate = oldC, oldD }()
	Commit, BuildDate = "a1cf7b5", "2026-10-07T13:54:00Z"
	if got := Build(); got != "a1cf7b5, 2026-10-07 13:54 UTC" {
		t.Errorf("Build = %q", got)
	}
	if got := Full(); !strings.HasPrefix(got, Version+" (a1cf7b5, 2026-10-07") {
		t.Errorf("Full = %q", got)
	}
	if got := ShortBuild(); got != Short()+" (a1cf7b5)" {
		t.Errorf("ShortBuild = %q", got)
	}
}
