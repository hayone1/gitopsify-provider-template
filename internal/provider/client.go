package provider

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hayone1/gitopsify/sdk"
)

// client talks to one instance of the tool.
type client struct {
	base  *url.URL
	token string
	http  *http.Client
}

// newClient checks the settings. Errors say which setting is wrong and how
// to fix it: gitopsify shows the message and the hint.
func newClient(settings map[string]string) (*client, error) {
	raw := strings.TrimSpace(settings["url"])
	if raw == "" {
		return nil, sdk.Errorf(sdk.CodeInvalidSettings, "url is empty").WithHint("set TEMPLATE_URL or the url setting")
	}
	u, err := url.Parse(raw)
	if err != nil {
		// Not echoed: it may hold credentials.
		return nil, sdk.Errorf(sdk.CodeInvalidSettings, "url isn't a URL: %v", errors.Unwrap(err))
	}
	if u.User != nil {
		return nil, sdk.Errorf(sdk.CodeInvalidSettings, "url %s holds credentials", u.Redacted()).WithHint("put the token in TEMPLATE_TOKEN, not in the URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		// Not echoed: "user:pass@host" parses with the scheme "user".
		return nil, sdk.Errorf(sdk.CodeInvalidSettings, "url isn't an http(s) URL").WithHint("for example https://notes.example.com")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	token := strings.TrimSpace(settings["token"])
	if token == "" {
		return nil, sdk.Errorf(sdk.CodeInvalidSettings, "token is empty").WithHint("set TEMPLATE_TOKEN or the token setting")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &client{base: u, token: token, http: &http.Client{Transport: tr, Timeout: 60 * time.Second}}, nil
}

// get fetches path (with its query) and decodes the JSON answer into out
// (nil: discard it). It retries rate limits, server errors and dropped
// connections.
func (c *client) get(ctx context.Context, path string, out any) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 500 * time.Millisecond):
			}
		}
		retry, err := c.try(ctx, path, out)
		if err == nil || !retry {
			return err
		}
		lastErr = err
	}
	return lastErr
}

// resolve returns the URL of path: a path on the server ("/api/info"), or a
// link the server returned (the next page). Links must point to the same
// server: the token goes with every request.
func (c *client) resolve(path string) (string, error) {
	if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
		return c.base.String() + path, nil
	}
	u, err := url.Parse(path)
	if err != nil || !sameServer(u, c.base) {
		return "", sdk.Errorf(sdk.CodeInternal, "the server sent a link to somewhere else: %.200q", path)
	}
	return u.String(), nil
}

// sameServer reports whether u is on the server base names: same scheme,
// host (in any case) and port, no credentials.
func sameServer(u, base *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return u.User == nil && strings.EqualFold(u.Scheme, base.Scheme) &&
		strings.EqualFold(u.Hostname(), base.Hostname()) && port(u) == port(base)
}

func (c *client) try(ctx context.Context, path string, out any) (retry bool, err error) {
	full, err := c.resolve(path)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gitopsify-provider-"+name)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		var cert *tls.CertificateVerificationError
		if errors.As(err, &cert) {
			return false, sdk.Errorf(sdk.CodeUnreachable, "GET %s: %v", path, err).WithHint("the server's TLS certificate isn't trusted")
		}
		hint := "check the URL and that the server is reachable from here"
		var dns *net.DNSError
		if errors.As(err, &dns) {
			hint = "the host name doesn't resolve; check the URL"
		}
		return true, sdk.Errorf(sdk.CodeUnreachable, "GET %s: %v", path, err).WithHint("%s", hint)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return true, sdk.Errorf(sdk.CodeUnreachable, "GET %s: reading the answer: %v", path, err)
	}
	if len(body) > maxBody {
		return false, sdk.Errorf(sdk.CodeInternal, "GET %s: the answer is larger than %d MiB", path, maxBody>>20)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 && s <= 60 {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(time.Duration(s) * time.Second):
			}
		}
		return true, httpError(path, resp.StatusCode, body)
	}
	if resp.StatusCode != http.StatusOK {
		return false, httpError(path, resp.StatusCode, body)
	}
	// Recorded under the request's path and query as sent (escaped), less
	// the base URL's path: what the test server sees.
	record(ctx, strings.TrimPrefix(req.URL.RequestURI(), c.base.EscapedPath()), body)
	if out == nil {
		return false, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, sdk.Errorf(sdk.CodeInternal, "GET %s: unexpected answer: %v", path, err)
	}
	return false, nil
}

// maxBody caps one answer: big, not unbounded.
const maxBody = 64 << 20

// httpError turns a failed request into an error with a code gitopsify
// understands and a hint that says what to do.
func httpError(path string, status int, body []byte) error {
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &msg)
	text := msg.Message
	if text == "" {
		text = strings.TrimSpace(string(body))
		if len(text) > 200 {
			text = text[:200] + "…"
		}
	}
	e := sdk.Errorf(sdk.CodeForHTTPStatus(status), "GET %s: HTTP %d: %s", path, status, text)
	switch status {
	case http.StatusUnauthorized:
		e = e.WithHint("the server refused the token; check TEMPLATE_TOKEN")
	case http.StatusForbidden:
		e = e.WithHint("the token can't read this; give it read access to every workspace")
	}
	return e
}

// page is one page of a list. next is the path of the next page, empty on
// the last one.
type page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next"`
}

// list fetches every page of a list.
func list[T any](ctx context.Context, c *client, path string) ([]T, error) {
	var all []T
	for n := 0; path != ""; n++ {
		if n == 10000 {
			return nil, sdk.Errorf(sdk.CodeInternal, "GET %s: more than 10000 pages", path)
		}
		var p page[T]
		if err := c.get(ctx, path, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Items...)
		if p.Next == path {
			return nil, sdk.Errorf(sdk.CodeInternal, "GET %s: the next page is the same page", path)
		}
		path = p.Next
	}
	return all, nil
}

// RecordEnv names a directory where every answer is saved, so the tests can
// replay a real instance (see testdata/README.md).
const RecordEnv = "GITOPSIFY_TEMPLATE_RECORD"

func record(ctx context.Context, path string, body []byte) {
	dir := os.Getenv(RecordEnv)
	if dir == "" {
		return
	}
	p := filepath.Join(dir, FixtureName(path))
	err := os.MkdirAll(filepath.Dir(p), 0o700)
	if err == nil {
		// Answers can hold secrets (the webhook URLs here): record only a
		// throwaway instance.
		err = os.WriteFile(p, body, 0o600)
	}
	if err != nil {
		sdk.Logger(ctx).Warn("recording " + path + ": " + err.Error())
	}
}

// FixtureName is where an answer is recorded: the path and query, all
// lowercase (uppercase letters become !<lowercase>, as in Go's module cache;
// everything but letters, digits, dots, dashes and underscores becomes %xx),
// so names never differ only in case and work on every OS and in Go module
// zips.
func FixtureName(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		switch c := path[i]; {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '.', c == '-', c == '_':
			b.WriteByte(c)
		case 'A' <= c && c <= 'Z':
			b.WriteByte('!')
			b.WriteByte(c + 'a' - 'A')
		default:
			fmt.Fprintf(&b, "%%%02x", c)
		}
	}
	name := b.String()
	if len(name) > 200 {
		sum := sha256.Sum256([]byte(path))
		name = name[:160] + "-" + hex.EncodeToString(sum[:8])
	}
	return name + ".json"
}
