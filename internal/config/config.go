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
		Name      string `yaml:"name"`
		Sysop     string `yaml:"sysop"`
		NewUserSL int    `yaml:"new_user_sl"`
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
