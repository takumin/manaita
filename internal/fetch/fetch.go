// Package fetch downloads files from their origin, or from the cache servers
// of a proxy list instead, verifying them against their pinned checksums.
//
// The proxy list has the syntax and the meaning of GOPROXY: a list of cache
// server URLs, "direct" for the origin and "off" to disallow downloading,
// separated by commas or pipes. After a comma, the next element is tried only
// when the element before answers 404 or 410; after a pipe, it is tried on any
// error. A cache server serves the origin URL https://host/path at
// <cache server URL>/host/path.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takumin/manaita/internal/logging"
)

// Direct is the proxy list fetching from the origin only.
const Direct = "direct"

// DialTimeout bounds the connection to a server, so that an unreachable
// cache server falls back to the next element instead of hanging.
const DialTimeout = 5 * time.Second

// maxRedirects is the redirects followed, as by the default http.Client.
const maxRedirects = 10

// source is an element of the proxy list.
type source struct {
	// base is the URL of the cache server, empty for the origin.
	base string
	// off disallows downloading.
	off bool
	// anyError tries the next element on any error, not only on not found.
	anyError bool
}

func (s source) String() string {
	switch {
	case s.off:
		return "off"
	case s.base == "":
		return Direct
	default:
		return s.base
	}
}

// Fetcher downloads the files through the sources of a proxy list.
type Fetcher struct {
	sources []source
	Client  *http.Client
}

// New returns a Fetcher through the proxy list, fetching from the origin when
// it is empty.
func New(proxy string) (*Fetcher, error) {
	sources, err := parse(proxy)
	if err != nil {
		return nil, err
	}
	return &Fetcher{sources: sources, Client: NewClient()}, nil
}

// NewClient returns an http.Client whose connections time out after
// DialTimeout. The proxy variables of the environment apply as usual.
func NewClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert
	t.DialContext = (&net.Dialer{Timeout: DialTimeout, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{Transport: t}
}

// parse returns the sources of the proxy list.
func parse(proxy string) ([]source, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return []source{{}}, nil
	}
	var sources []source
	for proxy != "" {
		elem, rest, sep := proxy, "", byte(0)
		if i := strings.IndexAny(proxy, ",|"); i >= 0 {
			elem, rest, sep = proxy[:i], proxy[i+1:], proxy[i]
		}
		elem = strings.TrimSpace(elem)
		var s source
		switch elem {
		case "":
			return nil, fmt.Errorf("invalid proxy list %q: empty element", proxy)
		case Direct:
		case "off":
			s.off = true
		default:
			u, err := url.Parse(elem)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
				return nil, fmt.Errorf("invalid proxy list: %q is not a cache server URL, %s or off", elem, Direct)
			}
			s.base = strings.TrimSuffix(elem, "/")
		}
		s.anyError = sep == '|'
		sources = append(sources, s)
		proxy = rest
		if sep != 0 && strings.TrimSpace(rest) == "" {
			return nil, fmt.Errorf("invalid proxy list: trailing %q", sep)
		}
	}
	return sources, nil
}

// Rewrite returns the URL of origin on the cache server base, or false when
// the cache server cannot serve it: only https URLs without a port are
// served.
func Rewrite(base, origin string) (string, bool) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Port() != "" || u.User != nil {
		return "", false
	}
	rewritten := strings.TrimSuffix(base, "/") + "/" + u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		rewritten += "?" + u.RawQuery
	}
	return rewritten, true
}

// StatusError is a response other than 200 OK.
type StatusError struct {
	URL    string
	Status string
	Code   int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("failed to download %s: %s", e.URL, e.Status)
}

// notFound reports whether err is a 404 or 410 response, after which the
// element following a comma is tried.
func notFound(err error) bool {
	var s *StatusError
	return errors.As(err, &s) && (s.Code == http.StatusNotFound || s.Code == http.StatusGone)
}

// File downloads origin to path through the proxy list, with the permissions
// perm. The file must match sum, its SHA-256 in hex; a mismatching file is an
// error like a failed download, and is never written to path.
func (f *Fetcher) File(ctx context.Context, origin, path, sum string, perm os.FileMode) error {
	return f.Get(ctx, origin, func(target string, body io.Reader) error {
		return writeFile(target, body, path, sum, perm)
	})
}

// Get downloads origin through the proxy list, passing the response body to
// read with the URL it comes from. An error of read, such as a checksum
// mismatch, is a failed download like any other, so read must leave nothing
// behind when it fails.
func (f *Fetcher) Get(ctx context.Context, origin string, read func(target string, body io.Reader) error) error {
	logger := logging.FromContext(ctx)
	var errs []error
	for i, s := range f.sources {
		if s.off {
			errs = append(errs, fmt.Errorf("failed to download %s: disallowed by the proxy list", origin))
			break
		}
		target := origin
		if s.base != "" {
			rewritten, ok := Rewrite(s.base, origin)
			if !ok {
				logger.DebugContext(ctx, "the cache server cannot serve the URL", slog.String("url", origin), slog.String("proxy", s.base))
				continue
			}
			target = rewritten
		}
		logger.DebugContext(ctx, "downloading", slog.String("url", target))
		err := f.download(ctx, target, s.base, read)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
		if i == len(f.sources)-1 || ctx.Err() != nil {
			break
		}
		if !s.anyError && !notFound(err) {
			break
		}
		level := slog.LevelWarn
		if notFound(err) {
			level = slog.LevelDebug
		}
		logger.Log(ctx, level, "trying the next source of the proxy list", slog.String("source", s.String()), slog.Any("error", err))
	}
	if len(errs) == 0 {
		return fmt.Errorf("failed to download %s: no source of the proxy list serves it", origin)
	}
	return errors.Join(errs...)
}

// download downloads target, passing the body to read. base is the cache
// server target is on, empty for the origin.
func (f *Fetcher) download(ctx context.Context, target, base string, read func(string, io.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	client := f.client(ctx, base)
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", target, err)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode != http.StatusOK {
		return &StatusError{URL: target, Status: res.Status, Code: res.StatusCode}
	}
	return read(target, res.Body)
}

// writeFile writes body, downloaded from target, to path through a temporary
// file, checking sum.
func writeFile(target string, body io.Reader, path, sum string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to download %s: %w", target, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("checksum mismatch for %s: want %s, got %s", target, sum, got)
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// client returns the client downloading from base. A cache server
// redirecting to another host, as GitHub releases do to their signed URLs,
// is followed, with a warning that the file did not come from the cache.
func (f *Fetcher) client(ctx context.Context, base string) *http.Client {
	c := *f.Client
	if base == "" {
		return &c
	}
	next := c.CheckRedirect
	warned := false
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !warned && req.URL.Host != via[0].URL.Host {
			warned = true
			logging.FromContext(ctx).WarnContext(ctx, "the cache server redirected to another host, so the file is not cached there",
				slog.String("url", via[0].URL.String()), slog.String("host", req.URL.Host))
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	return &c
}
