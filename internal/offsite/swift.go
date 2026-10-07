package offsite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
)

// OpenStack Swift, logged in through Keystone v3 with user and
// password, scoped to a project; the object store's public endpoint
// comes from the token's catalog (in the configured region).

type swiftTarget struct {
	http      *http.Client
	endpoint  string // .../v1/AUTH_<project>
	token     string
	container string
	prefix    string
}

// swiftHTTP may be replaced in tests.
var swiftHTTP = &http.Client{Timeout: 0}

func keystoneURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if !strings.HasSuffix(u, "/v3") {
		u += "/v3"
	}
	return u
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return strings.TrimSpace(v)
}

func openSwift(ctx context.Context, c config.OffsiteSwift) (Target, error) {
	if c.AuthURL == "" || c.User == "" || c.Password == "" || c.Container == "" {
		return nil, fmt.Errorf("offsite: Swift needs the auth URL, user, password and container")
	}
	body := map[string]any{"auth": map[string]any{
		"identity": map[string]any{
			"methods": []string{"password"},
			"password": map[string]any{"user": map[string]any{
				"name": c.User, "password": c.Password,
				"domain": map[string]string{"name": orDefault(c.UserDomain, "Default")},
			}},
		},
		"scope": map[string]any{"project": map[string]any{
			"name":   orDefault(c.Project, c.User),
			"domain": map[string]string{"name": orDefault(c.ProjectDomain, "Default")},
		}},
	}}
	raw, _ := json.Marshal(body)
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx2, http.MethodPost, keystoneURL(c.AuthURL)+"/auth/tokens", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	res, err := swiftHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("offsite: Keystone: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		return nil, fmt.Errorf("offsite: Keystone refused the login (HTTP %d): %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Token struct {
			Catalog []struct {
				Type      string `json:"type"`
				Endpoints []struct {
					Interface string `json:"interface"`
					Region    string `json:"region"`
					RegionID  string `json:"region_id"`
					URL       string `json:"url"`
				} `json:"endpoints"`
			} `json:"catalog"`
		} `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("offsite: Keystone's answer: %w", err)
	}
	t := &swiftTarget{http: swiftHTTP, token: res.Header.Get("X-Subject-Token"), container: c.Container,
		prefix: strings.TrimLeft(strings.TrimSpace(c.Prefix), "/")}
	for _, svc := range out.Token.Catalog {
		if svc.Type != "object-store" {
			continue
		}
		for _, e := range svc.Endpoints {
			if e.Interface == "public" && (c.Region == "" || e.Region == c.Region || e.RegionID == c.Region) {
				t.endpoint = strings.TrimRight(e.URL, "/")
				break
			}
		}
	}
	if t.endpoint == "" {
		return nil, fmt.Errorf("offsite: no object store (public, region %q) in the account's catalog", c.Region)
	}
	if t.token == "" {
		return nil, fmt.Errorf("offsite: Keystone sent no token")
	}
	// The container, made if it isn't there.
	if _, err := t.do(ctx, http.MethodPut, t.containerURL(), nil, http.StatusCreated, http.StatusAccepted, http.StatusNoContent); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *swiftTarget) containerURL() string {
	return t.endpoint + "/" + url.PathEscape(t.container)
}

func (t *swiftTarget) objectURL(name string) string {
	return t.containerURL() + "/" + strings.ReplaceAll(url.PathEscape(t.prefix+name), "%2F", "/")
}

func (t *swiftTarget) do(ctx context.Context, method, u string, body io.Reader, ok ...int) (*http.Response, error) {
	return t.doSized(ctx, method, u, body, -1, ok...)
}

func (t *swiftTarget) doSized(ctx context.Context, method, u string, body io.Reader, size int64, ok ...int) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if size >= 0 && body != nil {
		req.ContentLength = size
	}
	req.Header.Set("X-Auth-Token", t.token)
	res, err := t.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("offsite: Swift: %w", err)
	}
	for _, code := range ok {
		if res.StatusCode == code {
			return res, nil
		}
	}
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 1000))
	res.Body.Close()
	return nil, fmt.Errorf("offsite: Swift %s: HTTP %d %s", method, res.StatusCode, strings.TrimSpace(string(msg)))
}

func (t *swiftTarget) Put(ctx context.Context, name string, r io.Reader, size int64) error {
	res, err := t.doSized(ctx, http.MethodPut, t.objectURL(name), r, size, http.StatusCreated)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (t *swiftTarget) List(ctx context.Context) ([]string, error) {
	q := url.Values{"format": {"json"}, "prefix": {t.prefix + "nullmodem-"}}
	res, err := t.do(ctx, http.MethodGet, t.containerURL()+"?"+q.Encode(), nil, http.StatusOK, http.StatusNoContent)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	var objs []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&objs); err != nil {
		return nil, fmt.Errorf("offsite: Swift list: %w", err)
	}
	var out []string
	for _, o := range objs {
		n := strings.TrimPrefix(o.Name, t.prefix)
		if ours(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (t *swiftTarget) Delete(ctx context.Context, name string) error {
	res, err := t.do(ctx, http.MethodDelete, t.objectURL(name), nil, http.StatusNoContent, http.StatusNotFound, http.StatusOK)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (t *swiftTarget) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	res, err := t.do(ctx, http.MethodGet, t.objectURL(name), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (t *swiftTarget) Close() error { return nil }
