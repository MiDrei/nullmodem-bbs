// Package hostkey manages the SSH server's persistent host key,
// generating one on first run.
package hostkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// LoadOrCreate reads an existing PEM-encoded host key at path, or
// generates and persists a new ed25519 key there if none exists.
func LoadOrCreate(path string) (ssh.Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("hostkey: parse %s: %w", path, err)
		}
		return signer, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("hostkey: read %s: %w", path, err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("hostkey: generate: %w", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, "nullmodem bbs host key")
	if err != nil {
		return nil, fmt.Errorf("hostkey: marshal: %w", err)
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("hostkey: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, pemEncode(block), 0o600); err != nil {
		return nil, fmt.Errorf("hostkey: write %s: %w", path, err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("hostkey: signer: %w", err)
	}
	return signer, nil
}

func pemEncode(block *pem.Block) []byte {
	return pem.EncodeToMemory(block)
}
