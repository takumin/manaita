package fetch_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/logging"
)

func TestNew(t *testing.T) {
	cases := map[string]bool{
		"":                               true,
		"direct":                         true,
		"off":                            true,
		"http://cache.internal":          true,
		"https://cache.internal/mirror/": true,
		"http://a.internal,http://b.internal|direct": true,
		" http://cache.internal | direct ":           true,
		"http://cache.internal,":                     false,
		",direct":                                    false,
		"http://a.internal,,direct":                  false,
		"cache.internal":                             false,
		"ftp://cache.internal":                       false,
		"http://":                                    false,
		"http://cache.internal?a=b":                  false,
	}
	for proxy, valid := range cases {
		_, err := fetch.New(proxy)
		if valid && err != nil {
			t.Errorf("%q: unexpected error: %v", proxy, err)
		}
		if !valid && err == nil {
			t.Errorf("%q: expected an error", proxy)
		}
	}
}

func TestRewrite(t *testing.T) {
	cases := map[string]struct {
		base   string
		origin string
		want   string
	}{
		"path":         {"http://cache.internal", "https://github.com/a/b/c", "http://cache.internal/github.com/a/b/c"},
		"base path":    {"http://cache.internal/mirror/", "https://github.com/a", "http://cache.internal/mirror/github.com/a"},
		"query":        {"http://cache.internal", "https://example.com/a?x=1&y=2", "http://cache.internal/example.com/a?x=1&y=2"},
		"escaped":      {"http://cache.internal", "https://example.com/a%20b", "http://cache.internal/example.com/a%20b"},
		"http origin":  {"http://cache.internal", "http://example.com/a", ""},
		"port":         {"http://cache.internal", "https://example.com:8443/a", ""},
		"userinfo":     {"http://cache.internal", "https://u:p@example.com/a", ""},
		"not absolute": {"http://cache.internal", "/a", ""},
	}
	for name, tt := range cases {
		got, ok := fetch.Rewrite(tt.base, tt.origin)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("%s: want %q, got %q (%v)", name, tt.want, got, ok)
		}
	}
}

// originHost is the host of the origin, served by the origin test server.
const originHost = "origin.test"

// toOrigin sends the requests to originHost to the origin test server.
type toOrigin struct {
	origin *url.URL
	next   http.RoundTripper
}

func (rt *toOrigin) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == originHost {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = rt.origin.Scheme, rt.origin.Host
	}
	return rt.next.RoundTrip(req)
}

func TestFile(t *testing.T) {
	body := []byte("binary")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	serve := func(w http.ResponseWriter, _ *http.Request) { w.Write(body) } //nolint:errcheck,gosec
	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}
	tampered := func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("tampered")) } //nolint:errcheck,gosec
	redirect := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+originHost+"/signed?sig=1", http.StatusFound)
	}

	const origin = "https://" + originHost + "/releases/v1/bin?x=1"
	cases := map[string]struct {
		// proxy is the proxy list, with CACHE for the cache server URL and
		// DOWN for an unreachable one.
		proxy  string
		origin string
		cache  http.HandlerFunc
		ok     bool
		// caches and origins are the requests expected on each server.
		caches  int32
		origins int32
		warn    string
	}{
		"direct":                   {"direct", origin, nil, true, 0, 1, ""},
		"empty":                    {"", origin, nil, true, 0, 1, ""},
		"cache":                    {"CACHE", origin, serve, true, 1, 0, ""},
		"cache only not found":     {"CACHE", origin, status(http.StatusNotFound), false, 1, 0, ""},
		"comma not found":          {"CACHE,direct", origin, status(http.StatusNotFound), true, 1, 1, ""},
		"comma gone":               {"CACHE,direct", origin, status(http.StatusGone), true, 1, 1, ""},
		"comma server error":       {"CACHE,direct", origin, status(http.StatusBadGateway), false, 1, 0, ""},
		"comma tampered":           {"CACHE,direct", origin, tampered, false, 1, 0, ""},
		"comma off":                {"CACHE,off", origin, status(http.StatusNotFound), false, 1, 0, ""},
		"pipe server error":        {"CACHE|direct", origin, status(http.StatusBadGateway), true, 1, 1, "trying the next source"},
		"pipe tampered":            {"CACHE|direct", origin, tampered, true, 1, 1, "trying the next source"},
		"pipe unreachable":         {"DOWN|direct", origin, nil, true, 0, 1, "trying the next source"},
		"comma unreachable":        {"DOWN,direct", origin, nil, false, 0, 0, ""},
		"next cache":               {"DOWN|CACHE", origin, serve, true, 1, 0, ""},
		"off":                      {"off", origin, nil, false, 0, 0, ""},
		"redirect":                 {"CACHE", origin, redirect, true, 1, 1, "redirected to another host"},
		"not rewritable":           {"CACHE", "", serve, false, 0, 0, ""},
		"not rewritable to direct": {"CACHE,direct", "", serve, true, 0, 1, ""},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			var caches, origins atomic.Int32
			originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origins.Add(1)
				serve(w, r)
			}))
			defer originSrv.Close()
			cacheSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				caches.Add(1)
				if r.URL.Path != "/"+originHost+"/releases/v1/bin" || r.URL.RawQuery != "x=1" {
					t.Errorf("unexpected request to the cache server: %s", r.URL)
				}
				tt.cache(w, r)
			}))
			defer cacheSrv.Close()
			down := httptest.NewServer(http.NotFoundHandler())
			down.Close()

			u, err := url.Parse(originSrv.URL)
			if err != nil {
				t.Fatal(err)
			}
			proxy := strings.NewReplacer("CACHE", cacheSrv.URL, "DOWN", down.URL).Replace(tt.proxy)
			f, err := fetch.New(proxy)
			if err != nil {
				t.Fatal(err)
			}
			f.Client.Transport = &toOrigin{origin: u, next: f.Client.Transport}

			src := tt.origin
			if src == "" {
				src = originSrv.URL + "/releases/v1/bin?x=1"
			}
			var logs bytes.Buffer
			ctx := logging.NewContext(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
			path := filepath.Join(t.TempDir(), "cache", "bin")
			err = f.File(ctx, src, path, want, 0o644)
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error")
			}
			if caches.Load() != tt.caches || origins.Load() != tt.origins {
				t.Errorf("want %d requests to the cache and %d to the origin, got %d and %d", tt.caches, tt.origins, caches.Load(), origins.Load())
			}
			if tt.warn != "" && !strings.Contains(logs.String(), tt.warn) {
				t.Errorf("want a warning %q, got %q", tt.warn, logs.String())
			}

			got, err := os.ReadFile(path)
			switch {
			case tt.ok && !bytes.Equal(got, body):
				t.Errorf("unexpected file: %q (%v)", got, err)
			case !tt.ok && err == nil:
				t.Errorf("no file expected, got %q", got)
			}
		})
	}
}

func TestFilePerm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("binary")) //nolint:errcheck,gosec
	}))
	defer srv.Close()
	sum := sha256.Sum256([]byte("binary"))
	f, err := fetch.New(fetch.Direct)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bin")
	if err := f.File(context.Background(), srv.URL, path, hex.EncodeToString(sum[:]), 0o755); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("unexpected file: %v %v", info, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 1 {
		t.Errorf("want only the file, got %v (%v)", entries, err)
	}
}
