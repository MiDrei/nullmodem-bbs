package web

import "testing"

func TestPortOf(t *testing.T) {
	for addr, want := range map[string]string{
		":2323":         "2323",
		"0.0.0.0:2323":  "2323",
		"[::]:2222":     "2222",
		"":              "",
		"no-port-given": "",
	} {
		if got := portOf(addr); got != want {
			t.Errorf("portOf(%q) = %q, want %q", addr, got, want)
		}
	}
}
