package doors

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMRCConfigRoundTrip checks mrc.cfg's layout against uMRC v106's
// struct settings (common/common.h): 1067 bytes, the SSL flag at
// offset 86, the BBS name right after it.
func TestMRCConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := DefaultMRCConfig("Maiks Place BBS", "SwissMaik")
	c.Website = "https://bbs.maik.ch"
	c.Telnet = "bbs.maik.ch:2323"
	if err := WriteMRCConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, MRCConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1067 || data[86] != 1 || string(data[87:102]) != "Maiks Place BBS" {
		t.Fatalf("unexpected layout: %d bytes, ssl byte %d, name %q", len(data), data[86], data[87:102])
	}
	got, err := ReadMRCConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != c {
		t.Fatalf("read back %+v, want %+v", got, c)
	}
}

func TestMRCConfigRejectsTooLongField(t *testing.T) {
	c := DefaultMRCConfig("x", "y")
	c.Port = "123456"
	if err := c.Validate(); err == nil {
		t.Fatal("a 6-character port was accepted; mrc.cfg has room for 5")
	}
}
