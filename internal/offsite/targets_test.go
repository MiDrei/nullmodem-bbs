package offsite

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"golang.org/x/net/webdav"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
)

// roundTrip runs Test, then a real copy with pruning, against cfg.
func roundTrip(t *testing.T, cfg config.OffsiteConfig) {
	t.Helper()
	ctx := context.Background()
	if err := Test(ctx, cfg); err != nil {
		t.Fatalf("test: %v", err)
	}
	priv, pub, _ := NewKey()
	cfg.Enabled, cfg.Recipient, cfg.KeepDaily, cfg.KeepWeekly = true, pub, ptr(1), ptr(0)
	dir := t.TempDir()
	bc := config.BackupConfig{Dir: dir, Offsite: cfg}
	sqlDB, _ := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	defer sqlDB.Close()
	c := &Copier{DB: sqlDB, Logger: quiet{}, Config: func() config.BackupConfig { return bc }}
	old := writeBackup(t, dir, time.Now().Add(-72*time.Hour), "old one")
	if _, err := c.Copy(ctx, bc, old); err != nil {
		t.Fatal(err)
	}
	newest := writeBackup(t, dir, time.Now(), strings.Repeat("newest backup ", 1000))
	if _, err := c.Copy(ctx, bc, newest); err != nil {
		t.Fatal(err)
	}
	tg, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer tg.Close()
	names, _ := tg.List(ctx)
	if len(names) != 1 || names[0] != newest.Name+Suffix {
		t.Fatalf("there: %v", names)
	}
	r, err := tg.Get(ctx, names[0])
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := io.ReadAll(r)
	r.Close()
	id, _ := age.ParseX25519Identity(priv)
	dr, err := age.Decrypt(bytes.NewReader(enc), id)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(dr)
	want, _ := os.ReadFile(filepath.Join(dir, newest.Name))
	if !bytes.Equal(got, want) {
		t.Fatal("decrypted copy differs")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("temporary files left in the backup dir: %d entries", len(entries))
	}
}

func TestWebDAV(t *testing.T) {
	h := &webdav.Handler{FileSystem: webdav.Dir(t.TempDir()), LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "box" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	roundTrip(t, config.OffsiteConfig{Kind: "webdav", WebDAV: config.OffsiteWebDAV{URL: srv.URL + "/remote.php/dav/bbs backups", User: "box", Password: "pw"}})
	if err := Test(context.Background(), config.OffsiteConfig{Kind: "webdav", WebDAV: config.OffsiteWebDAV{URL: srv.URL + "/x", User: "box", Password: "no"}}); err == nil || !strings.Contains(err.Error(), "refused the login") {
		t.Fatalf("wrong password: %v", err)
	}
}

// TestS3 needs an S3 service with the key minioadmin/minioadmin, e.g.
// SeaweedFS ("weed server -s3 -s3.config=...") or MinIO, on HTTP:
// OFFSITE_S3_TEST=127.0.0.1:9000 go test ./internal/offsite
func TestS3(t *testing.T) {
	ep := os.Getenv("OFFSITE_S3_TEST")
	if ep == "" {
		t.Skip("OFFSITE_S3_TEST not set")
	}
	roundTrip(t, config.OffsiteConfig{Kind: "s3", S3: config.OffsiteS3{Endpoint: ep, Region: "us-east-1", Bucket: "nullmodem-test",
		AccessKey: "minioadmin", SecretKey: "minioadmin", Prefix: "bbs/", PathStyle: true, Insecure: true}})
	err := Test(context.Background(), config.OffsiteConfig{Kind: "s3", S3: config.OffsiteS3{Endpoint: ep, Bucket: "nullmodem-test",
		AccessKey: "minioadmin", SecretKey: "wrong", PathStyle: true, Insecure: true}})
	if err == nil || !strings.Contains(err.Error(), "secret key is wrong") {
		t.Fatalf("wrong key: %v", err)
	}
}
