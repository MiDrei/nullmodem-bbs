package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WebConfig holds settings for cmd/web, the admin API/UI daemon.
type WebConfig struct {
	Addr          string `yaml:"addr"`
	DatabasePath  string `yaml:"database_path"`
	BBSConfigPath string `yaml:"bbs_config_path"`
	JWTSecretPath string `yaml:"jwt_secret_path"`
	StaticDir     string `yaml:"static_dir"`
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
