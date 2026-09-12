package web

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// LoadOrCreateJWTSecret reads a hex-encoded signing secret from path,
// or generates and persists a new random 32-byte secret there if none
// exists yet.
func LoadOrCreateJWTSecret(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		secret, err := hex.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("web: parse jwt secret %s: %w", path, err)
		}
		return secret, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("web: read jwt secret %s: %w", path, err)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("web: generate jwt secret: %w", err)
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("web: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(secret)), 0o600); err != nil {
		return nil, fmt.Errorf("web: write jwt secret %s: %w", path, err)
	}
	return secret, nil
}
