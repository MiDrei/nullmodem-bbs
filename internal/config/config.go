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
		// FTNAddress is this system's own FidoNet address
		// (zone:net/node.point), stamped as the From address on
		// outgoing netmail. Optional and empty by default -- there's
		// no BinkP mailer yet (see internal/netmail's doc comment),
		// so it has no effect beyond that display/bookkeeping until
		// one exists.
		FTNAddress string `yaml:"ftn_address"`
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
	return c, nil
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
