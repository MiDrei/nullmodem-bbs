package web

import (
	"bytes"
	"encoding/xml"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
)

func TestShareImageMetaAndFeeds(t *testing.T) {
	srv, _, configPath := newTestServer(t)
	static := t.TempDir()
	os.WriteFile(filepath.Join(static, "index.html"), []byte("<html><head><title>x</title></head><body></body></html>"), 0o644)
	srv.StaticDir = static
	h := srv.Routes()
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "bbs.example"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// The welcome screen as the preview picture.
	rec := get("/og-image.png")
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if rec.Code != http.StatusOK || err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
		t.Fatalf("og image: %d %v", rec.Code, err)
	}

	// Any app page carries the preview tags; no feeds while off.
	page := get("/").Body.String()
	for _, want := range []string{`og:title" content="Test BBS"`, `og:image" content="https://bbs.example/og-image.png"`, "<title>x</title>"} {
		if !strings.Contains(page, want) {
			t.Errorf("index lacks %s:\n%s", want, page)
		}
	}
	if strings.Contains(page, "application/rss+xml") || get("/feeds/general.xml").Code != http.StatusNotFound {
		t.Error("feeds while they're off")
	}

	// On: a feed per area a new caller may read.
	c, _ := config.Load(configPath)
	c.BBS.PublicFeeds = true
	config.Save(configPath, c)
	general, _ := srv.Messages.AreaByTag("general")
	srv.Messages.ReceiveEcho(general.ID, "Bob", "Hello <world>", "\x1b[1;31mred\x1b[0m text & more", "1:1/1 1", time.Now())
	secret, _ := srv.Messages.CreateArea("sysops", "Sysops", "", "", 200, 200)
	_ = secret
	if page := get("/").Body.String(); !strings.Contains(page, `href="https://bbs.example/feeds/general.xml"`) || strings.Contains(page, "sysops.xml") {
		t.Errorf("feed links: %s", page)
	}
	rec = get("/feeds/general.xml")
	var feed rss
	if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("feed: %d %v\n%s", rec.Code, err, rec.Body)
	}
	if len(feed.Channel.Items) != 1 || feed.Channel.Items[0].Title != "Hello <world>" || strings.Contains(feed.Channel.Items[0].Description, "\x1b") {
		t.Fatalf("items: %+v", feed.Channel.Items)
	}
	if get("/feeds/sysops.xml").Code != http.StatusNotFound {
		t.Error("a sysop area has a public feed")
	}
}
