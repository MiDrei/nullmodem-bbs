package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestPublicFiles(t *testing.T) {
	srv, users, _ := newTestServer(t)
	static := t.TempDir()
	os.WriteFile(filepath.Join(static, "index.html"), []byte("<html><head></head><body></body></html>"), 0o644)
	srv.StaticDir = static
	u, _ := users.Register("root", "supersecret", user.SLSysop)
	area, _ := srv.Files.CreateArea("pub", "Public Stuff", "", "", 0, 10)
	f, err := srv.Files.UploadFile(area.ID, u.ID, "TOOL.ZIP", "A handy tool\nfor sysops", strings.NewReader("zipdata"))
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Routes()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	dl := "/dl/" + itoa(f.ID) + "/TOOL.ZIP"

	// Not public: nothing.
	if get("/api/public/files/"+itoa(f.ID)).Code != http.StatusNotFound || get(dl).Code != http.StatusNotFound {
		t.Fatal("a file of a private area is out")
	}
	srv.Files.SetAreaPublic(area.ID, true)

	rec := get("/api/public/files")
	var list []publicFileDTO
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Summary != "A handy tool" || list[0].Page != "/share/f/"+itoa(f.ID) {
		t.Fatalf("list: %s", rec.Body)
	}
	rec = get(dl)
	if rec.Code != http.StatusOK || rec.Body.String() != "zipdata" || !strings.Contains(rec.Header().Get("Content-Disposition"), "TOOL.ZIP") {
		t.Fatalf("download: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	if g, _ := srv.Files.FileByID(f.ID); g.DownloadCount != 1 {
		t.Errorf("downloads %d", g.DownloadCount)
	}
	page := get("/share/f/" + itoa(f.ID)).Body.String()
	if !strings.Contains(page, `og:title" content="TOOL.ZIP"`) || !strings.Contains(page, "A handy tool -- ") {
		t.Errorf("share page preview:\n%s", page)
	}
}
