package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// WebConfig holds settings for cmd/web, the admin API/UI daemon.
type WebConfig struct {
	Addr          string `yaml:"addr"`
	DatabasePath  string `yaml:"database_path"`
	BBSConfigPath string `yaml:"bbs_config_path"`
	JWTSecretPath string `yaml:"jwt_secret_path"`
	StaticDir     string `yaml:"static_dir"`
	// TerminalAddr is the bbs daemon's Telnet port the web terminal
	// connects to ("bbs:2323" in Docker); empty: this machine, at the
	// port in bbs.yaml.
	TerminalAddr string `yaml:"terminal_addr,omitempty"`
	// MetricsToken is the bearer token a monitoring server sends for
	// GET /metrics; empty (the default) turns the endpoint off. The
	// environment variable NULLMODEM_METRICS_TOKEN, when set, wins.
	MetricsToken string `yaml:"metrics_token,omitempty"`
}

// MetricsTokenEnv names the environment variable that sets the
// metrics token instead of web.yaml.
const MetricsTokenEnv = "NULLMODEM_METRICS_TOKEN"

// Metrics returns the token for GET /metrics: the environment
// variable's, else web.yaml's; "" leaves the endpoint off.
func (c *WebConfig) Metrics() string {
	if v := strings.TrimSpace(os.Getenv(MetricsTokenEnv)); v != "" {
		return v
	}
	return strings.TrimSpace(c.MetricsToken)
}

// DefaultWeb returns the built-in web daemon configuration.
func DefaultWeb() *WebConfig {
	return &WebConfig{
		Addr:          ":8090",
		DatabasePath:  "data/nullmodem.sqlite",
		BBSConfigPath: "configs/bbs.yaml",
		JWTSecretPath: "data/jwt_secret",
		StaticDir:     "web/build",
	}
}

// LoadWeb reads and parses a YAML web config file at path.
func LoadWeb(path string) (*WebConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	c := DefaultWeb()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return c, nil
}
