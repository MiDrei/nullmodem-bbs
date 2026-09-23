package user

import "testing"

func TestIsRestrictedUsername(t *testing.T) {
	cases := map[string]bool{
		"sysop": true, "SysOp": true, "  admin  ": true, "root": true,
		"alice": false, "": false, "sysopalice": false,
	}
	for in, want := range cases {
		if got := IsRestrictedUsername(in); got != want {
			t.Errorf("IsRestrictedUsername(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsRestrictedRealName(t *testing.T) {
	cases := map[string]bool{
		"System Operator": true, "  Guest  ": true, "sysop": true,
		"Alice Example": false, "": false,
	}
	for in, want := range cases {
		if got := IsRestrictedRealName(in); got != want {
			t.Errorf("IsRestrictedRealName(%q) = %v, want %v", in, got, want)
		}
	}
}
