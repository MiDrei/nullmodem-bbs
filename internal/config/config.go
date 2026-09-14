// Package config loads the BBS daemon's YAML configuration.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds settings for cmd/bbs.
type Config struct {
	BBS struct {
		Name       string `yaml:"name"`
		Sysop      string `yaml:"sysop"`
		NewUserSL  int    `yaml:"new_user_sl"`
		MenusDir   string `yaml:"menus_dir"`
		ScreensDir string `yaml:"screens_dir"`
		FilesDir   string `yaml:"files_dir"`
		// FTNAddresses are this system's own FTN addresses (AKAs) on
		// whichever FTN-compatible network(s) it belongs to (FidoNet,
		// fsxNet, etc.) -- most systems have exactly one, but a point
		// reachable through the same uplink under more than one
		// network needs to identify with all of them in the same
		// BinkP session (see internal/tosser and internal/binkp's
		// OurAddresses). The first address is "primary": it's stamped
		// as the From address on locally composed netmail and shown
		// as this system's main address in the web UI. Optional and
		// empty by default -- not every BBS is on an FTN network.
		FTNAddresses []string `yaml:"ftn_addresses"`
	} `yaml:"bbs"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`

	Telnet struct {
		Enabled bool   `yaml:"enabled"`
		Addr    string `yaml:"addr"`
	} `yaml:"telnet"`

	SSH struct {
		Enabled     bool   `yaml:"enabled"`
		Addr        string `yaml:"addr"`
		HostKeyPath string `yaml:"host_key_path"`
	} `yaml:"ssh"`

	Binkp struct {
		// Uplinks are the BinkP nodes/hubs this system polls to
		// exchange netmail (see internal/tosser). Echomail routing
		// isn't implemented yet -- only netmail is tossed so far.
		Uplinks []BinkpUplink `yaml:"uplinks"`
		// PollIntervalSeconds is the default interval cmd/mailer waits
		// between polls of an uplink that doesn't set its own
		// BinkpUplink.PollIntervalSeconds. The web admin's "Send Now"
		// button (see internal/web) polls on demand regardless of
		// this interval.
		PollIntervalSeconds int `yaml:"poll_interval_seconds"`
		// ListenEnabled and ListenAddr configure cmd/mailer's inbound
		// BinkP listener (a caller dialing us, e.g. a hub that wants to
		// push mail between our own scheduled polls rather than
		// waiting for us to ask -- see tosser.Answer). A caller is
		// authenticated by matching its claimed FTN address against
		// Uplinks, so only a system already configured as one of our
		// uplinks can push mail to us; nothing else is accepted.
		ListenEnabled bool   `yaml:"listen_enabled"`
		ListenAddr    string `yaml:"listen_addr"`
	} `yaml:"binkp"`
}

// BinkpUplink is one BinkP node/hub this system connects out to.
type BinkpUplink struct {
	// Address is the uplink's own FTN address, for display/reference
	// only -- it's not cross-checked against what the uplink actually
	// claims via M_ADR when connecting.
	Address string `yaml:"address"`
	// Host is "host:port", e.g. "bbs.example.com:24554".
	Host string `yaml:"host"`
	// Password authenticates the BinkP *session* itself (sent as a
	// CRAM-MD5 response if the uplink advertises support, otherwise
	// in the clear -- see internal/binkp.Config.Password), distinct
	// from PacketPassword below. Empty means no session password is
	// sent.
	Password string `yaml:"password"`
	// PollDisabled excludes this uplink from cmd/mailer's regular,
	// interval-based scheduled poll -- for a link that should only
	// ever be dialed when there's actually something to send (any
	// pending netmail or echomail routed to it -- see cmd/mailer's
	// crash-style triggering, not just Crash-flagged netmail
	// specifically), plus its own PollIntervalSeconds as a slow
	// fallback if set (needed for a link with no way to push mail
	// to us the way internal/tosser.Answer lets a configured
	// uplink do -- otherwise it would only ever receive anything
	// when Crash-flagged netmail happened to be routed to it), or
	// purely on demand via the web admin's "Send Now" button
	// (which ignores this flag entirely).
	PollDisabled bool `yaml:"poll_disabled"`
	// PollIntervalSeconds overrides Binkp.PollIntervalSeconds for
	// this uplink specifically -- some hubs only permit polling every
	// hour or two and reject a caller that connects more often (see
	// internal/tosser's IsDue). Zero means "use the global default".
	// Meaningless when PollDisabled is set.
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	// PacketPassword authenticates the FTS-0001 packet itself (the
	// 8-byte password field in a .pkt file's own header -- see
	// internal/mail.PacketHeader), independent of the BinkP session
	// Password above. internal/tosser stamps it on every outbound
	// packet built for this uplink, and rejects an inbound packet
	// whose own header password doesn't match. Empty means no packet
	// password is set or expected.
	PacketPassword string `yaml:"packet_password"`
	// TICPassword authenticates TIC file-echo announcements from this
	// uplink (the "type 2" file-distribution protocol layered over
	// BinkP). Reserved for when TIC/file-echo support is implemented
	// in internal/tosser -- stored but not used yet.
	TICPassword string `yaml:"tic_password"`
	// AreafixPassword authenticates automated echomail area
	// subscription requests to/from this uplink's "AREAFIX" netmail
	// robot. Reserved for when Areafix support is implemented in
	// internal/tosser -- stored but not used yet.
	AreafixPassword string `yaml:"areafix_password"`
	// Network labels which FTN network this uplink carries echomail
	// for (e.g. "fsxNet", "HobbyNet"), matched case-insensitively
	// against a message area's own Network field (set by the sysop
	// when approving/editing an area -- see internal/message.Area) to
	// decide which uplink a locally-posted echo message goes out
	// through. Empty means this uplink never carries any locally-
	// originated echomail (fine for a network we only read, never
	// post to).
	Network string `yaml:"network"`
}

// PrimaryFTNAddress returns c's first configured FTN address, or "" if
// none are configured -- the address stamped on locally composed
// netmail and shown as this system's main address, for callers that
// only care about the single "our own address" case rather than the
// full AKA list a BinkP session presents.
func (c *Config) PrimaryFTNAddress() string {
	if len(c.BBS.FTNAddresses) == 0 {
		return ""
	}
	return c.BBS.FTNAddresses[0]
}

// Default returns the built-in configuration used when no config file
// is present, so the BBS is runnable out of the box.
func Default() *Config {
	c := &Config{}
	c.BBS.Name = "NullModem BBS"
	c.BBS.Sysop = "sysop"
	c.BBS.NewUserSL = 10
	c.BBS.MenusDir = "configs/menus"
	c.BBS.ScreensDir = "configs/screens"
	c.BBS.FilesDir = "data/files"
	c.Database.Path = "data/nullmodem.sqlite"
	c.Telnet.Enabled = true
	c.Telnet.Addr = ":2323"
	c.SSH.Enabled = true
	c.SSH.Addr = ":2222"
	c.SSH.HostKeyPath = "data/ssh_host_key"
	c.Binkp.PollIntervalSeconds = 900
	return c
}

// Load reads and parses a YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	c := Default()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	migrateLegacyFTNAddress(data, c)
	return c, nil
}

// migrateLegacyFTNAddress folds an already-deployed config's old
// singular "bbs.ftn_address" key into FTNAddresses if that list came
// back empty, so upgrading to the list form (see FTNAddresses' doc
// comment) doesn't silently drop an address someone already
// configured. The next Save rewrites the file in the new shape,
// dropping the legacy key for good.
func migrateLegacyFTNAddress(data []byte, c *Config) {
	if len(c.BBS.FTNAddresses) > 0 {
		return
	}
	var legacy struct {
		BBS struct {
			FTNAddress string `yaml:"ftn_address"`
		} `yaml:"bbs"`
	}
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return
	}
	if legacy.BBS.FTNAddress != "" {
		c.BBS.FTNAddresses = []string{legacy.BBS.FTNAddress}
	}
}

// Save marshals c as YAML and writes it to path, overwriting any
// existing file. Used by the web admin API to persist edits made
// through the config UI.
func Save(path string, c *Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}
