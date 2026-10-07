package bbs

import (
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/menu"
)

// The menu editor offers menu.Builtins: exactly the commands there are.
func TestMenuCatalogMatchesBuiltins(t *testing.T) {
	for _, b := range menu.Builtins {
		if _, ok := builtins[b.Name]; !ok {
			t.Errorf("menu.Builtins has %q, the BBS doesn't", b.Name)
		}
		if b.Sysop != sysopBuiltins[b.Name] {
			t.Errorf("%q: sysop %v in the catalog, %v here", b.Name, b.Sysop, sysopBuiltins[b.Name])
		}
	}
	for name := range builtins {
		if _, ok := menu.BuiltinByName(name); !ok {
			t.Errorf("builtin %q is missing from menu.Builtins", name)
		}
	}
}
