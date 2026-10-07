package offsite

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/midrei/nullmodem-bbs/internal/backup"
	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
)

type quiet struct{}

func (quiet) Info(string, ...any) {}
func (quiet) Warn(string, ...any) {}

// fakeSwift is Keystone v3 plus a Swift container, in memory.
func fakeSwift(t *testing.T) (*httptest.Server, map[string][]byte) {
	objs := map[string][]byte{}
	var mu sync.Mutex
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/identity/v3/auth/tokens" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			pw := body["auth"].(map[string]any)["identity"].(map[string]any)["password"].(map[string]any)["user"].(map[string]any)["password"]
			if pw != "secret" {
				http.Error(w, `{"error":{"message":"The request you have made requires authentication."}}`, 401)
				return
			}
			w.Header().Set("X-Subject-Token", "tok")
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{"token": map[string]any{"catalog": []any{
				map[string]any{"type": "object-store", "endpoints": []any{
					map[string]any{"interface": "public", "region": "RegionOne", "url": srv.URL + "/object/v1/AUTH_p"},
				}},
			}}})
			return
		}
		if r.Header.Get("X-Auth-Token") != "tok" {
			w.WriteHeader(401)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/object/v1/AUTH_p/")
		switch {
		case r.Method == http.MethodPut && key == "bbs":
			w.WriteHeader(202)
		case r.Method == http.MethodGet && key == "bbs":
			var list []map[string]string
			for k := range objs {
				name := strings.TrimPrefix(k, "bbs/")
				if strings.HasPrefix(name, r.URL.Query().Get("prefix")) {
					list = append(list, map[string]string{"name": name})
				}
			}
			sort.Slice(list, func(i, j int) bool { return list[i]["name"] < list[j]["name"] })
			json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			objs[key] = data
			w.WriteHeader(201)
		case r.Method == http.MethodGet:
			data, ok := objs[key]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Write(data)
		case r.Method == http.MethodDelete:
			delete(objs, key)
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, objs
}

func writeBackup(t *testing.T, dir string, at time.Time, content string) backup.Info {
	name := "nullmodem-" + at.Format("20060102-150405") + ".tar.gz"
	os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	return backup.Info{Name: name, Time: at}
}

func TestCopyToSwiftEncryptsAndPrunes(t *testing.T) {
	srv, objs := fakeSwift(t)
	sqlDB, _ := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	defer sqlDB.Close()
	priv, pub, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.BackupConfig{Dir: dir, Offsite: config.OffsiteConfig{Enabled: true, Kind: "swift", Recipient: pub,
		KeepDaily: ptr(1), KeepWeekly: ptr(0),
		Swift: config.OffsiteSwift{AuthURL: srv.URL + "/identity", User: "u", Password: "secret", Project: "p", Container: "bbs"}}}
	if err := Test(context.Background(), cfg.Offsite); err != nil {
		t.Fatalf("test: %v", err)
	}
	c := &Copier{DB: sqlDB, Logger: quiet{}, Config: func() config.BackupConfig { return cfg }}
	old := writeBackup(t, dir, time.Now().Add(-48*time.Hour), "old backup")
	if _, err := c.Copy(context.Background(), cfg, old); err != nil {
		t.Fatal(err)
	}
	newest := writeBackup(t, dir, time.Now(), "the newest backup")
	c.tick(context.Background())
	st := LoadStatus(sqlDB)
	if st.LastName != newest.Name || st.LastError != "" || st.Remote != 1 {
		t.Fatalf("status %+v", st)
	}
	if len(objs) != 1 {
		t.Fatalf("objects there: %d (the older one should be pruned)", len(objs))
	}
	enc := objs["bbs/"+newest.Name+Suffix]
	if bytes.Contains(enc, []byte("newest")) {
		t.Fatal("stored in the clear")
	}
	id, _ := age.ParseX25519Identity(priv)
	r, err := age.Decrypt(bytes.NewReader(enc), id)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(r); string(got) != "the newest backup" {
		t.Fatalf("decrypted %q", got)
	}
	// A wrong password: a clear error, kept in the status.
	cfg.Offsite.Swift.Password = "nope"
	writeBackup(t, dir, time.Now().Add(time.Minute), "next")
	c.RetryAfter = time.Nanosecond
	c.tick(context.Background())
	if st := LoadStatus(sqlDB); !strings.Contains(st.LastError, "refused the login") {
		t.Fatalf("status %+v", st)
	}
}

func ptr(n int) *int { return &n }

// sftpServer runs an SSH server with the sftp subsystem, accepting key.
func sftpServer(t *testing.T, root string, allow ssh.PublicKey) (string, int, string) {
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(hostPriv)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(k.Marshal(), allow.Marshal()) {
			return nil, nil
		}
		return nil, io.EOF
	}}
	cfg.AddHostKey(hostSigner)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, in, _ := nc.Accept()
					go func() {
						for req := range in {
							req.Reply(req.Type == "subsystem", nil)
							if req.Type == "subsystem" {
								s, _ := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
								s.Serve()
								ch.Close()
							}
						}
					}()
				}
			}()
		}
	}()
	return "127.0.0.1", ln.Addr().(*net.TCPAddr).Port, ssh.FingerprintSHA256(hostSigner.PublicKey())
}

func TestSFTPWithPinnedHostKey(t *testing.T) {
	key, line, err := NewSSHKey("nullmodem-test")
	if err != nil || !strings.HasPrefix(line, "ssh-ed25519 ") {
		t.Fatalf("key: %v %q", err, line)
	}
	signer, _ := ssh.ParsePrivateKey([]byte(key))
	root := t.TempDir()
	host, port, fp := sftpServer(t, root, signer.PublicKey())
	c := config.OffsiteConfig{Kind: "sftp", SFTP: config.OffsiteSFTP{Host: host, Port: port, User: "u", Key: key, Dir: "backups"}}

	if err := Test(context.Background(), c); err != ErrHostKeyUnknown {
		t.Fatalf("unpinned: %v", err)
	}
	got, err := ScanHostKey(context.Background(), c.SFTP)
	if err != nil || got != fp {
		t.Fatalf("scan: %v %q %q", err, got, fp)
	}
	c.SFTP.HostKey = got
	if err := Test(context.Background(), c); err != nil {
		t.Fatalf("test: %v", err)
	}
	tg, err := Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	tg.Put(context.Background(), "nullmodem-20261003-030000.tar.gz.age", strings.NewReader("x"), 1)
	names, _ := tg.List(context.Background())
	tg.Close()
	if len(names) != 1 {
		t.Fatalf("listed %v", names)
	}
	c.SFTP.HostKey = "SHA256:somebodyelse"
	if err := Test(context.Background(), c); err == nil || !strings.Contains(err.Error(), "key changed") {
		t.Fatalf("a different server: %v", err)
	}
}
