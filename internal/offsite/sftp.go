package offsite

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"git.maik.ch/nullmodem/bbs/internal/config"
)

// ErrHostKeyUnknown: the server's key isn't confirmed yet (HostKey
// empty); ScanHostKey learns it.
var ErrHostKeyUnknown = errors.New("offsite: the server's key isn't confirmed yet -- test the connection in the admin first")

type sftpTarget struct {
	ssh *ssh.Client
	c   *sftp.Client
	dir string
}

func sftpAddr(c config.OffsiteSFTP) string {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(strings.TrimSpace(c.Host), strconv.Itoa(port))
}

func sftpAuth(c config.OffsiteSFTP) ([]ssh.AuthMethod, error) {
	var auth []ssh.AuthMethod
	if c.Key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(c.Key))
		if err != nil {
			return nil, fmt.Errorf("offsite: the SSH key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if c.Password != "" {
		auth = append(auth, ssh.Password(c.Password))
	}
	if len(auth) == 0 {
		return nil, errors.New("offsite: SFTP needs a password or a key")
	}
	return auth, nil
}

func dialSSH(ctx context.Context, c config.OffsiteSFTP, check ssh.HostKeyCallback) (*ssh.Client, error) {
	auth, err := sftpAuth(c)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{User: c.User, Auth: auth, HostKeyCallback: check, Timeout: 20 * time.Second}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", sftpAddr(c))
	if err != nil {
		return nil, fmt.Errorf("offsite: %w", err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, sftpAddr(c), cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("offsite: %w", err)
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

// ScanHostKey connects (logging in) and returns the server's key's
// SHA256 fingerprint, for the sysop to confirm and pin.
func ScanHostKey(ctx context.Context, c config.OffsiteSFTP) (string, error) {
	var seen string
	client, err := dialSSH(ctx, c, func(_ string, _ net.Addr, key ssh.PublicKey) error {
		seen = ssh.FingerprintSHA256(key)
		return nil
	})
	if err != nil {
		return seen, err
	}
	client.Close()
	return seen, nil
}

func openSFTP(ctx context.Context, c config.OffsiteSFTP) (Target, error) {
	if c.HostKey == "" {
		return nil, ErrHostKeyUnknown
	}
	client, err := dialSSH(ctx, c, func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if got := ssh.FingerprintSHA256(key); got != c.HostKey {
			return fmt.Errorf("the server's key changed (%s, expected %s) -- if that's right, confirm it again in the admin", got, c.HostKey)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("offsite: sftp: %w", err)
	}
	dir := strings.TrimSpace(c.Dir)
	if dir == "" {
		dir = "nullmodem-backups"
	}
	if err := sc.MkdirAll(dir); err != nil {
		sc.Close()
		client.Close()
		return nil, fmt.Errorf("offsite: making %s: %w", dir, err)
	}
	return &sftpTarget{ssh: client, c: sc, dir: dir}, nil
}

func (t *sftpTarget) Put(ctx context.Context, name string, r io.Reader) error {
	tmp := path.Join(t.dir, name+".part")
	f, err := t.c.Create(tmp)
	if err != nil {
		return fmt.Errorf("offsite: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		t.c.Remove(tmp)
		return fmt.Errorf("offsite: uploading: %w", err)
	}
	if err := f.Close(); err != nil {
		t.c.Remove(tmp)
		return fmt.Errorf("offsite: %w", err)
	}
	final := path.Join(t.dir, name)
	t.c.Remove(final)
	if err := t.c.Rename(tmp, final); err != nil {
		return fmt.Errorf("offsite: %w", err)
	}
	return nil
}

func (t *sftpTarget) List(ctx context.Context) ([]string, error) {
	entries, err := t.c.ReadDir(t.dir)
	if err != nil {
		return nil, fmt.Errorf("offsite: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && ours(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func (t *sftpTarget) Delete(ctx context.Context, name string) error {
	if err := t.c.Remove(path.Join(t.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("offsite: %w", err)
	}
	return nil
}

func (t *sftpTarget) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	f, err := t.c.Open(path.Join(t.dir, name))
	if err != nil {
		return nil, fmt.Errorf("offsite: %w", err)
	}
	return f, nil
}

func (t *sftpTarget) Close() error {
	t.c.Close()
	return t.ssh.Close()
}

// NewSSHKey makes an ed25519 key for logging in to the SFTP server:
// the private key (OpenSSH format, kept in bbs.yaml) and the public
// line to put into the server's authorized_keys.
func NewSSHKey(comment string) (private, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " " + comment
	return string(pem.EncodeToMemory(block)), line, nil
}

// PublicKeyOf is the authorized_keys line of a stored private key.
func PublicKeyOf(private, comment string) string {
	signer, err := ssh.ParsePrivateKey([]byte(private))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + comment
}
