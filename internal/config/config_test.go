package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadMigratesLegacySingularFTNAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bbs.yaml")
	writeFile(t, path, "bbs:\n    name: Test BBS\n    ftn_address: \"21:3/194.1\"\n")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"21:3/194.1"}
	if !reflect.DeepEqual(c.BBS.FTNAddresses, want) {
		t.Fatalf("FTNAddresses = %v, want %v", c.BBS.FTNAddresses, want)
	}
	if got := c.PrimaryFTNAddress(); got != "21:3/194.1" {
		t.Fatalf("PrimaryFTNAddress() = %q, want %q", got, "21:3/194.1")
	}
}

func TestLoadPrefersNewListOverLegacySingularField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bbs.yaml")
	writeFile(t, path, "bbs:\n    name: Test BBS\n    ftn_address: \"21:3/194.1\"\n    ftn_addresses:\n        - \"1:234/56\"\n")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"1:234/56"}
	if !reflect.DeepEqual(c.BBS.FTNAddresses, want) {
		t.Fatalf("FTNAddresses = %v, want %v (new list should win, not merge with the legacy field)", c.BBS.FTNAddresses, want)
	}
}

func TestPrimaryFTNAddressEmptyWhenUnconfigured(t *testing.T) {
	c := Default()
	if got := c.PrimaryFTNAddress(); got != "" {
		t.Fatalf("PrimaryFTNAddress() = %q, want empty for a fresh default config", got)
	}
}

func TestSaveAndLoadRoundTripsMultipleFTNAddresses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bbs.yaml")
	c := Default()
	c.BBS.FTNAddresses = []string{"21:3/194.1", "954:700/14"}

	if err := Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(reloaded.BBS.FTNAddresses, c.BBS.FTNAddresses) {
		t.Fatalf("reloaded FTNAddresses = %v, want %v", reloaded.BBS.FTNAddresses, c.BBS.FTNAddresses)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
}
