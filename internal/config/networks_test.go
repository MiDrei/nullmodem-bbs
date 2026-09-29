package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// apolloLike is the shape of the live config before networks existed.
const apolloLike = `bbs:
    name: Test
    ftn_addresses:
        - 21:3/194@fsxnet
        - 954:700/14@hobbynet
        - 1337:1/131@tqwnet
binkp:
    uplinks:
        - address: 21:3/100
          network: fsxNet Echo Areas
          aka_addresses: [21:3/194, 954:700/14, 21:3/194@fsxnet, 954:700/14@hobbynet]
        - address: 954:700/1
          network: HobbyNet Echo Areas
          aka_addresses: [21:3/194, 954:700/14, 21:3/194@fsxnet, 954:700/14@hobbynet]
        - address: 1337:1/100
          network: tqwNet Echo Areas
          aka_addresses: [1337:1/131@tqwnet]
        - address: 9999:1/100
          network: NullModem Echo Areas
          aka_addresses: []
        - address: 1:2/3
          network: ""
`

func loadString(t *testing.T, yaml string) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bbs.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoadDerivesNetworksFromLegacyUplinkGroups(t *testing.T) {
	c := loadString(t, apolloLike)
	want := []Network{
		{"fsxNet", "fsxnet"},
		{"HobbyNet", "hobbynet"},
		{"tqwNet", "tqwnet"},
		{"NullModem", "nullmodem"}, // no address in zone 9999: from the name
	}
	if !reflect.DeepEqual(c.Networks, want) {
		t.Fatalf("Networks = %+v, want %+v", c.Networks, want)
	}
	for i, name := range []string{"fsxNet", "HobbyNet", "tqwNet", "NullModem", ""} {
		if c.Binkp.Uplinks[i].Network != name {
			t.Errorf("uplink %d network = %q, want %q", i, c.Binkp.Uplinks[i].Network, name)
		}
	}
	for old, new := range map[string]string{
		"fsxNet Echo Areas":    "fsxNet",
		"fsxNet File Areas":    "fsxNet",
		"NullModem Echo Areas": "NullModem",
		"HobbyNet File Areas":  "HobbyNet",
	} {
		if c.NetworkRenames[old] != new {
			t.Errorf("NetworkRenames[%q] = %q, want %q", old, c.NetworkRenames[old], new)
		}
	}
}

func TestSavedNetworksAreNotMigratedAgain(t *testing.T) {
	c := loadString(t, apolloLike)
	p := filepath.Join(t.TempDir(), "bbs.yaml")
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.NetworkRenames) != 0 {
		t.Fatalf("a saved config was migrated again: %v", again.NetworkRenames)
	}
	if !reflect.DeepEqual(again.Networks, c.Networks) || again.Binkp.Uplinks[0].Network != "fsxNet" {
		t.Fatalf("round trip lost the networks: %+v", again.Networks)
	}
}

func TestShortNetworkName(t *testing.T) {
	for in, want := range map[string]string{
		"fsxNet Echo Areas":   "fsxNet",
		"HobbyNet File Areas": "HobbyNet",
		"FidoNet":             "FidoNet",
		"Retro Areas":         "Retro",
		"Areas":               "Areas",
	} {
		if got := shortNetworkName(in); got != want {
			t.Errorf("shortNetworkName(%q) = %q, want %q", in, got, want)
		}
	}
}

type fakeStore struct{ renames [][2]string }

func (f *fakeStore) RenameNetwork(from, to string) (int64, error) {
	f.renames = append(f.renames, [2]string{from, to})
	return 1, nil
}

func TestApplyNetworkRenamesVisitsEveryStore(t *testing.T) {
	c := &Config{NetworkRenames: map[string]string{"b Echo Areas": "b", "a Echo Areas": "a"}}
	m, f := &fakeStore{}, &fakeStore{}
	n, err := c.ApplyNetworkRenames(m, f)
	if err != nil || n != 4 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	if m.renames[0][0] != "a Echo Areas" || len(f.renames) != 2 {
		t.Fatalf("renames = %v / %v", m.renames, f.renames)
	}
}
