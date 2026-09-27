package doors

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Install limits: a door archive is a few MB; anything far bigger is
// not what we meant to download.
const (
	maxDownloadBytes = 64 << 20
	maxUnpackedBytes = 256 << 20
	maxArchiveFiles  = 5000
)

// ErrAlreadyInstalled is returned by Install when the door's
// directory already has files in it -- installing never overwrites a
// door someone set up (and its players' save games).
var ErrAlreadyInstalled = errors.New("doors: door directory already exists and is not empty")

// Install downloads t into doorsDir/t.Dir and prepares it to run: the
// DDPlus control file (if any) set to this board's name and sysop, and
// the template's own Prepare step.
// It returns the door's directory.
func Install(ctx context.Context, client *http.Client, t Template, doorsDir, bbsName, sysopName string) (string, error) {
	if t.Download == nil {
		return "", fmt.Errorf("doors: %s has no download; install it by hand", t.Name)
	}
	if !filepath.IsLocal(t.Dir) {
		return "", fmt.Errorf("doors: %s: bad directory %q", t.Name, t.Dir)
	}
	dest := filepath.Join(doorsDir, t.Dir)
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return "", ErrAlreadyInstalled
	}

	url, subdir, err := t.Download.resolve(runtime.GOARCH)
	if err != nil {
		return "", fmt.Errorf("doors: %s: %w", t.Name, err)
	}
	data, err := download(ctx, client, url)
	if err != nil {
		return "", err
	}

	// Unpack into a scratch directory next to dest first, so a failed
	// install leaves nothing half-done behind.
	if err := os.MkdirAll(doorsDir, 0o755); err != nil {
		return "", fmt.Errorf("doors: creating %s: %w", doorsDir, err)
	}
	tmp, err := os.MkdirTemp(doorsDir, ".install-"+t.Dir+"-*")
	if err != nil {
		return "", fmt.Errorf("doors: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	// Absolute, since Prepare steps run programs from it with it as
	// their working directory too (exec.Cmd resolves a relative
	// program path against Dir).
	if tmp, err = filepath.Abs(tmp); err != nil {
		return "", fmt.Errorf("doors: %w", err)
	}

	switch t.Download.Format {
	case "zip":
		err = unpackZip(data, tmp, subdir)
	case "tar.gz":
		err = unpackTarGz(data, tmp, subdir)
	default:
		err = fmt.Errorf("doors: unknown archive format %q", t.Download.Format)
	}
	if err != nil {
		return "", err
	}

	for _, exe := range t.Download.Executables {
		if err := os.Chmod(filepath.Join(tmp, exe), 0o755); err != nil {
			return "", fmt.Errorf("doors: %s: %w", t.Name, err)
		}
	}
	if t.Download.CtlFile != "" {
		if p, ok := findCaseInsensitive(tmp, t.Download.CtlFile); ok {
			if err := setCtlNames(p, bbsName, sysopName); err != nil {
				return "", err
			}
		}
	}

	if t.Download.Prepare != nil {
		if err := t.Download.Prepare(ctx, tmp, bbsName, sysopName); err != nil {
			return "", err
		}
	}

	os.Remove(dest) // an empty leftover directory, if any
	if err := os.Rename(tmp, dest); err != nil {
		return "", fmt.Errorf("doors: moving %s into place: %w", t.Name, err)
	}
	return dest, nil
}

// resolve fills "{arch}" in the download's URL and Subdir for goarch,
// or fails when the door has no build for it.
func (d *Download) resolve(goarch string) (url, subdir string, err error) {
	if len(d.Arch) == 0 {
		return d.URL, d.Subdir, nil
	}
	name, ok := d.Arch[goarch]
	if !ok {
		return "", "", fmt.Errorf("no build for %s", goarch)
	}
	return strings.ReplaceAll(d.URL, "{arch}", name), strings.ReplaceAll(d.Subdir, "{arch}", name), nil
}

// Supports reports whether the download has a build for goarch.
func (d *Download) Supports(goarch string) bool {
	_, _, err := d.resolve(goarch)
	return err == nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("doors: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doors: downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doors: downloading %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("doors: downloading %s: %w", url, err)
	}
	if len(data) > maxDownloadBytes {
		return nil, fmt.Errorf("doors: %s is larger than %d MB", url, maxDownloadBytes>>20)
	}
	return data, nil
}

// unpackTarget maps an archive entry name to where it goes under
// dest, or "" to skip it: only entries inside subdir (if set), with
// subdir itself stripped, and never anything escaping dest.
func unpackTarget(dest, name, subdir string) (string, bool) {
	name = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, `\`, "/")), "/")
	if subdir != "" {
		prefix := strings.Trim(subdir, "/") + "/"
		if !strings.HasPrefix(name, prefix) {
			return "", false
		}
		name = strings.TrimPrefix(name, prefix)
	}
	if name == "" || name == "." || !filepath.IsLocal(name) {
		return "", false
	}
	return filepath.Join(dest, filepath.FromSlash(name)), true
}

// writeEntry creates target with r's content, executable if mode
// (the archive's own) says so, counting against the unpacked-size
// budget.
func writeEntry(target string, r io.Reader, mode os.FileMode, budget *int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755 // keep an executable executable, nothing else
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, *budget+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	*budget -= n
	if *budget < 0 {
		return fmt.Errorf("doors: archive unpacks to more than %d MB", maxUnpackedBytes>>20)
	}
	return nil
}

func unpackZip(data []byte, dest, subdir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("doors: reading zip: %w", err)
	}
	if len(zr.File) > maxArchiveFiles {
		return fmt.Errorf("doors: zip has more than %d files", maxArchiveFiles)
	}
	budget := int64(maxUnpackedBytes)
	for _, f := range zr.File {
		target, ok := unpackTarget(dest, f.Name, subdir)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("doors: zip entry %s: %w", f.Name, err)
		}
		err = writeEntry(target, rc, f.Mode(), &budget)
		rc.Close()
		if err != nil {
			return fmt.Errorf("doors: unpacking %s: %w", f.Name, err)
		}
	}
	return nil
}

func unpackTarGz(data []byte, dest, subdir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("doors: reading tar.gz: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	budget := int64(maxUnpackedBytes)
	for files := 0; ; files++ {
		if files > maxArchiveFiles {
			return fmt.Errorf("doors: archive has more than %d files", maxArchiveFiles)
		}
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("doors: reading tar.gz: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue // directories are created as needed; links are never followed
		}
		target, ok := unpackTarget(dest, h.Name, subdir)
		if !ok {
			continue
		}
		if err := writeEntry(target, tr, os.FileMode(h.Mode), &budget); err != nil {
			return fmt.Errorf("doors: unpacking %s: %w", h.Name, err)
		}
	}
}

// findCaseInsensitive finds name directly in dir, ignoring case.
func findCaseInsensitive(dir, name string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name()), true
		}
	}
	return "", false
}

// setCtlNames sets a DDPlus control file's SYSOPFIRST, SYSOPLAST and
// BBSNAME lines to this board's own.
func setCtlNames(path, bbsName, sysopName string) error {
	first, last := splitName(sysopName)
	return setCtlValues(path, map[string]string{"SYSOPFIRST": first, "SYSOPLAST": last, "BBSNAME": bbsName})
}

// setCtlValues sets keyword lines (active, i.e. not ;-commented) of a
// DDPlus-style control file, keeping everything else as it was; a
// keyword the file doesn't have yet is appended.
func setCtlValues(path string, values map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("doors: reading %s: %w", filepath.Base(path), err)
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := strings.ToUpper(fields[0])
		if v, ok := values[key]; ok {
			lines[i] = strings.TrimSpace(key + " " + v)
			seen[key] = true
		}
	}
	for _, key := range []string{"SYSOPFIRST", "SYSOPLAST", "BBSNAME", "BBSTYPE"} {
		if v, ok := values[key]; ok && !seen[key] {
			lines = append(lines, strings.TrimSpace(key+" "+v))
		}
	}
	out := strings.Join(lines, "\r\n") + "\r\n"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("doors: writing %s: %w", filepath.Base(path), err)
	}
	return nil
}
