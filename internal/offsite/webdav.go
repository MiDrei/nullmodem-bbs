package offsite

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/config"
)

// WebDAV: a folder on Nextcloud, ownCloud, kDrive, a Storage Box ...
// PUT, GET, DELETE, PROPFIND to list, MKCOL to make the folder.

type webdavTarget struct {
	base       *url.URL // the folder, ending in "/"
	user, pass string
}

var webdavHTTP = &http.Client{}

func openWebDAV(ctx context.Context, c config.OffsiteWebDAV) (Target, error) {
	if c.URL == "" {
		return nil, errors.New("offsite: WebDAV needs the folder's URL")
	}
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("offsite: %q isn't a WebDAV URL (https://...)", c.URL)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	t := &webdavTarget{base: u, user: c.User, pass: c.Password}
	// The folder (and its parents), made if missing.
	res, err := t.req(ctx, "PROPFIND", u.String(), nil, -1, map[string]string{"Depth": "0"})
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	switch res.StatusCode {
	case http.StatusMultiStatus, http.StatusOK:
	case http.StatusNotFound:
		if err := t.mkcolAll(ctx); err != nil {
			return nil, err
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("offsite: WebDAV refused the login (HTTP %d)", res.StatusCode)
	default:
		return nil, fmt.Errorf("offsite: WebDAV: HTTP %d for the folder", res.StatusCode)
	}
	return t, nil
}

func (t *webdavTarget) req(ctx context.Context, method, u string, body io.Reader, size int64, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if t.user != "" || t.pass != "" {
		req.SetBasicAuth(t.user, t.pass)
	}
	if size >= 0 && body != nil {
		req.ContentLength = size
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := webdavHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("offsite: WebDAV: %w", err)
	}
	return res, nil
}

// mkcolAll makes the folder, parent by parent, from the first one
// that's missing.
func (t *webdavTarget) mkcolAll(ctx context.Context) error {
	parts := strings.Split(strings.Trim(t.base.Path, "/"), "/")
	cur := *t.base
	for i := range parts {
		cur.Path = "/" + strings.Join(parts[:i+1], "/") + "/"
		res, err := t.req(ctx, "MKCOL", cur.String(), nil, -1, nil)
		if err != nil {
			return err
		}
		res.Body.Close()
		// 405: there already; 409/403 on a parent we may not touch: go on.
		if res.StatusCode >= 500 {
			return fmt.Errorf("offsite: WebDAV: making %s: HTTP %d", cur.Path, res.StatusCode)
		}
	}
	return nil
}

func (t *webdavTarget) file(name string) string {
	u := *t.base
	u.Path = path.Join(t.base.Path, name)
	return u.String()
}

func (t *webdavTarget) Put(ctx context.Context, name string, r io.Reader, size int64) error {
	res, err := t.req(ctx, http.MethodPut, t.file(name), r, size, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return fmt.Errorf("offsite: WebDAV upload: HTTP %d %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

type multistatus struct {
	Responses []struct {
		Href string `xml:"href"`
	} `xml:"response"`
}

func (t *webdavTarget) List(ctx context.Context) ([]string, error) {
	res, err := t.req(ctx, "PROPFIND", t.base.String(), strings.NewReader(`<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`), -1,
		map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("offsite: WebDAV list: HTTP %d", res.StatusCode)
	}
	var ms multistatus
	if err := xml.NewDecoder(res.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("offsite: WebDAV list: %w", err)
	}
	var out []string
	for _, r := range ms.Responses {
		h, err := url.PathUnescape(r.Href)
		if err != nil {
			h = r.Href
		}
		if n := path.Base(strings.TrimRight(h, "/")); ours(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (t *webdavTarget) Delete(ctx context.Context, name string) error {
	res, err := t.req(ctx, http.MethodDelete, t.file(name), nil, -1, nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("offsite: WebDAV delete: HTTP %d", res.StatusCode)
	}
	return nil
}

func (t *webdavTarget) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	res, err := t.req(ctx, http.MethodGet, t.file(name), nil, -1, nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("offsite: WebDAV read: HTTP %d", res.StatusCode)
	}
	return res.Body, nil
}

func (t *webdavTarget) Close() error { return nil }
