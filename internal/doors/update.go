package doors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// VersionFile, in a door's directory, holds the release installed
// there (Install and Update write it).
const VersionFile = ".nullmodem-version"

func writeVersion(dir, version string) error {
	if version == "" {
		return nil
	}
	if err := os.WriteFile(filepath.Join(dir, VersionFile), []byte(version+"\n"), 0o644); err != nil {
		return fmt.Errorf("doors: writing %s: %w", VersionFile, err)
	}
	return nil
}

// InstalledVersion is the release of t installed in dir: from its
// version file, else the template's LegacyVersion; "" if it can't be
// told (or nothing is installed).
func InstalledVersion(t Template, dir string) string {
	if t.Download == nil || dir == "" {
		return ""
	}
	if b, err := os.ReadFile(filepath.Join(dir, VersionFile)); err == nil {
		return strings.TrimSpace(string(b))
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) == 0 {
		return ""
	}
	return t.Download.LegacyVersion
}

// Release is a door's newest release as its project publishes it.
type Release struct {
	Version string    `json:"version"`
	URL     string    `json:"url"` // the release notes
	At      time.Time `json:"at"`
}

// githubAPI is where LatestRelease asks (a test server in tests).
var githubAPI = "https://api.github.com"

// LatestRelease asks GitHub for t's newest release (pre-releases and
// drafts left out).
func LatestRelease(ctx context.Context, client *http.Client, t Template) (Release, error) {
	if t.Download == nil || t.Download.GitHub == "" {
		return Release{}, errors.New("doors: no release feed for " + t.Name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI+"/repos/"+t.Download.GitHub+"/releases/latest", nil)
	if err != nil {
		return Release{}, fmt.Errorf("doors: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("doors: asking GitHub about %s: %w", t.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("doors: asking GitHub about %s: %s", t.Name, resp.Status)
	}
	var body struct {
		Tag       string    `json:"tag_name"`
		URL       string    `json:"html_url"`
		Published time.Time `json:"published_at"`
		Assets    []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("doors: reading GitHub's answer about %s: %w", t.Name, err)
	}
	if body.Tag == "" {
		return Release{}, fmt.Errorf("doors: GitHub names no release of %s", t.Name)
	}
	// A release is published before its builds are uploaded: until
	// this system's is there, it isn't one to update to.
	url, _, err := t.Download.resolve(runtime.GOARCH, body.Tag)
	if err != nil {
		return Release{}, fmt.Errorf("doors: %s: %w", t.Name, err)
	}
	asset := path.Base(url)
	for _, a := range body.Assets {
		if a.Name == asset {
			return Release{Version: body.Tag, URL: body.URL, At: body.Published}, nil
		}
	}
	return Release{}, fmt.Errorf("doors: %s %s has no %s (yet)", t.Name, body.Tag, asset)
}

// SaveRelease records what the update check found for template id;
// LatestReleases reads it back.
func SaveRelease(db *sql.DB, id string, r Release, checkErr error, now time.Time) error {
	msg := ""
	if checkErr != nil {
		msg = checkErr.Error()
		// A failed check keeps the release found before.
		_, err := db.Exec(`INSERT INTO door_releases (template, checked_at, error) VALUES (?, ?, ?)
			ON CONFLICT(template) DO UPDATE SET checked_at = excluded.checked_at, error = excluded.error`,
			id, now.UnixMilli(), msg)
		return err
	}
	_, err := db.Exec(`INSERT INTO door_releases (template, version, url, published_at, checked_at, error) VALUES (?, ?, ?, ?, ?, '')
		ON CONFLICT(template) DO UPDATE SET version = excluded.version, url = excluded.url,
			published_at = excluded.published_at, checked_at = excluded.checked_at, error = ''`,
		id, r.Version, r.URL, r.At.UnixMilli(), now.UnixMilli())
	return err
}

// KnownRelease is the last update check's result for a template.
type KnownRelease struct {
	Release
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error"`
}

// LatestReleases returns the recorded releases by template ID.
func LatestReleases(db *sql.DB) (map[string]KnownRelease, error) {
	rows, err := db.Query(`SELECT template, version, url, published_at, checked_at, error FROM door_releases`)
	if err != nil {
		return nil, fmt.Errorf("doors: %w", err)
	}
	defer rows.Close()
	out := map[string]KnownRelease{}
	for rows.Next() {
		var id string
		var k KnownRelease
		var at, checked int64
		if err := rows.Scan(&id, &k.Version, &k.URL, &at, &checked, &k.Error); err != nil {
			return nil, fmt.Errorf("doors: %w", err)
		}
		if at > 0 {
			k.At = time.UnixMilli(at)
		}
		k.CheckedAt = time.UnixMilli(checked)
		out[id] = k
	}
	return out, rows.Err()
}

// UpdateAvailable reports whether latest is a release other than the
// installed one. Projects tag their releases in their own ways, so it
// only compares, ignoring a leading "v": the feed's latest release is
// newer by definition.
func UpdateAvailable(installed, latest string) bool {
	norm := func(v string) string { return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v") }
	return installed != "" && latest != "" && norm(installed) != norm(latest)
}

// Update brings the door t installed in dir to release version: it
// replaces the files the release ships and leaves everything else --
// save games, settings, logs -- as it is. The files it replaces are
// kept in a backup directory next to the door's (the previous one
// overwritten), and nothing of the door's own set-up is run again.
// A running door keeps its old program until it ends: files are
// swapped by renaming, never rewritten in place.
func Update(ctx context.Context, client *http.Client, t Template, dir, version string) error {
	if t.Download == nil {
		return fmt.Errorf("doors: %s has no download", t.Name)
	}
	if version == "" {
		return errors.New("doors: no version to update to")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) == 0 {
		return fmt.Errorf("doors: %s is not installed in %s", t.Name, dir)
	}
	parent := filepath.Dir(dir)
	removeStale(filepath.Join(parent, ".update-"+filepath.Base(dir)+"-*"))
	tmp, err := os.MkdirTemp(parent, ".update-"+filepath.Base(dir)+"-*")
	if err != nil {
		return fmt.Errorf("doors: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := fetch(ctx, client, t, version, tmp); err != nil {
		return err
	}
	if err := writeVersion(tmp, version); err != nil {
		return err
	}

	var files []string
	err = filepath.WalkDir(tmp, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(tmp, p)
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return fmt.Errorf("doors: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("doors: the %s release %s has no files", t.Name, version)
	}

	backup := filepath.Join(parent, ".backup-"+filepath.Base(dir))
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("doors: clearing the old backup: %w", err)
	}
	for _, rel := range files {
		old := filepath.Join(dir, rel)
		if info, err := os.Lstat(old); err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := copyFile(old, filepath.Join(backup, rel)); err != nil {
			return fmt.Errorf("doors: backing up %s: %w", rel, err)
		}
	}
	for _, rel := range files {
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("doors: %w", err)
		}
		if err := os.Rename(filepath.Join(tmp, rel), target); err != nil {
			return fmt.Errorf("doors: putting %s in place: %w", rel, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, info.Mode().Perm())
}

// removeStale deletes what pattern matches that is older than an hour:
// scratch directories a crash or restart left behind.
func removeStale(pattern string) {
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && time.Since(info.ModTime()) > time.Hour {
			os.RemoveAll(m)
		}
	}
}
