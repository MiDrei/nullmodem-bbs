package config

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Network is one FTN network this system belongs to. Name is the short
// label shown everywhere (tabs, the Telnet area list's dividers) and
// what a BinkpUplink's and an area's Network field refer to; Domain is
// its FTN domain, as in this system's own 5D addresses
// ("21:3/194@fsxnet"), used to tell which address belongs to which
// network.
type Network struct {
	Name   string `yaml:"name"`
	Domain string `yaml:"domain"`
}

// NetworkByName returns the network called name (compared without
// regard to case).
func (c *Config) NetworkByName(name string) (Network, bool) {
	for _, n := range c.Networks {
		if strings.EqualFold(n.Name, name) {
			return n, true
		}
	}
	return Network{}, false
}

// AddressDomain is the domain of an FTN address ("21:3/194@fsxnet" ->
// "fsxnet"), lower case, or "" when it has none.
func AddressDomain(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(strings.TrimSpace(addr[i+1:]))
	}
	return ""
}

// addressZone is the zone of an FTN address ("21:3/194" -> 21), or -1.
func addressZone(addr string) int {
	i := strings.IndexByte(addr, ':')
	if i <= 0 {
		return -1
	}
	z, err := strconv.Atoi(strings.TrimSpace(addr[:i]))
	if err != nil {
		return -1
	}
	return z
}

// legacyGroupSuffix matches the " Echo Areas"/" File Areas" style
// suffix group names used to carry before networks had short names.
var legacyGroupSuffix = regexp.MustCompile(`(?i)\s+(echo|file|message|msg)?\s*areas?$`)

// shortNetworkName strips a legacy group suffix: "fsxNet Echo Areas"
// -> "fsxNet".
func shortNetworkName(group string) string {
	if short := strings.TrimSpace(legacyGroupSuffix.ReplaceAllString(group, "")); short != "" {
		return short
	}
	return strings.TrimSpace(group)
}

// migrateLegacyNetworks derives Networks from a config written before
// they existed: each uplink's Network group ("fsxNet Echo Areas")
// becomes a network with the short name ("fsxNet") and the domain of
// this system's address in the uplink's zone ("21:3/194@fsxnet"), and
// the uplink is pointed at it. The old group names -- and their
// " File Areas" sibling -- are recorded in NetworkRenames so the
// caller can rename the areas in the database the same way; nothing is
// written to disk here. A config that already has Networks, or whose
// uplinks name none, is left alone.
func migrateLegacyNetworks(c *Config) {
	if len(c.Networks) > 0 {
		return
	}
	renames := map[string]string{}
	for i, u := range c.Binkp.Uplinks {
		if strings.TrimSpace(u.Network) == "" {
			continue
		}
		short := shortNetworkName(u.Network)
		if _, ok := c.NetworkByName(short); !ok {
			c.Networks = append(c.Networks, Network{Name: short, Domain: c.legacyDomain(u, short)})
		}
		for _, old := range []string{u.Network, short + " Echo Areas", short + " File Areas"} {
			if old != short {
				renames[old] = short
			}
		}
		c.Binkp.Uplinks[i].Network = short
	}
	if len(renames) > 0 {
		c.NetworkRenames = renames
	}
}

// legacyDomain guesses a network's domain for migrateLegacyNetworks:
// the domain of this system's own address (the uplink's own AKA list
// first, then all of them) in the uplink's zone, else the short name.
func (c *Config) legacyDomain(u BinkpUplink, short string) string {
	zone := addressZone(u.Address)
	for _, list := range [][]string{u.AKAAddresses, c.BBS.FTNAddresses} {
		for _, a := range list {
			if d := AddressDomain(a); d != "" && addressZone(a) == zone {
				return d
			}
		}
	}
	return strings.ToLower(strings.ReplaceAll(short, " ", ""))
}

// NetworkRenamer is a store whose areas carry a network name -- see
// ApplyNetworkRenames.
type NetworkRenamer interface {
	RenameNetwork(from, to string) (int64, error)
}

// ApplyNetworkRenames renames the areas in stores after
// NetworkRenames, returning how many changed. Every daemon calls it at
// startup; it only changes rows still carrying an old name, so running
// it again, or from several daemons at once, is harmless.
func (c *Config) ApplyNetworkRenames(stores ...NetworkRenamer) (int64, error) {
	olds := make([]string, 0, len(c.NetworkRenames))
	for old := range c.NetworkRenames {
		olds = append(olds, old)
	}
	sort.Strings(olds)
	var total int64
	for _, old := range olds {
		for _, st := range stores {
			n, err := st.RenameNetwork(old, c.NetworkRenames[old])
			if err != nil {
				return total, err
			}
			total += n
		}
	}
	return total, nil
}
