package mitamae_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/project"
)

func TestArch(t *testing.T) {
	cases := map[string]string{
		"x86_64":    "x86_64",
		"amd64":     "x86_64",
		"aarch64\n": "aarch64",
		"arm64":     "aarch64",
		"armv7l":    "armhf",
		"i686":      "i386",
	}
	for in, want := range cases {
		if got, err := mitamae.Arch(in); err != nil || got != want {
			t.Errorf("%q: want %s, got %s (%v)", in, want, got, err)
		}
	}
	if _, err := mitamae.Arch("riscv64"); err == nil {
		t.Error("expected an error")
	}
}

func TestLocalArch(t *testing.T) {
	if _, err := mitamae.LocalArch(); err != nil {
		t.Error(err)
	}
}

func TestNewFetcher(t *testing.T) {
	f, err := mitamae.NewFetcher(project.Mitamae{Version: "2.0.3"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != "2.0.3" || f.BaseURL == "" || f.Client == nil || f.CacheDir == "" {
		t.Errorf("unexpected fetcher: %+v", f)
	}
}

func TestFetch(t *testing.T) {
	body := []byte("#!/bin/sh\n")
	sum := sha256.Sum256(body)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v2.0.3/mitamae-x86_64-linux" {
			http.NotFound(w, r)
			return
		}
		w.Write(body) //nolint:errcheck,gosec
	}))
	defer srv.Close()

	f := &mitamae.Fetcher{
		Version: "v2.0.3",
		Checksums: map[string]string{
			"x86_64":  "sha256:" + hex.EncodeToString(sum[:]),
			"aarch64": hex.EncodeToString(sum[:]),
			"armhf":   "deadbeef",
		},
		CacheDir: t.TempDir(),
		BaseURL:  srv.URL,
		Client:   srv.Client(),
	}
	ctx := context.Background()

	path, err := f.Fetch(ctx, "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(f.CacheDir, "v2.0.3", "mitamae-x86_64-linux") || path != f.Path("x86_64") {
		t.Errorf("unexpected path: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("unexpected binary: %v %v", info, err)
	}
	if _, err := f.Fetch(ctx, "x86_64"); err != nil || requests.Load() != 1 {
		t.Errorf("expected the cached binary, got %d requests (%v)", requests.Load(), err)
	}

	if _, err := f.Fetch(ctx, "aarch64"); err == nil {
		t.Error("expected a not found error")
	}
	if _, err := f.Fetch(ctx, "i386"); err == nil {
		t.Error("expected a missing checksum error")
	}
	f.Checksums["armhf"] = "deadbeef"
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }) //nolint:errcheck,gosec
	if _, err := f.Fetch(ctx, "armhf"); err == nil {
		t.Error("expected a checksum mismatch")
	}
	if _, err := os.Stat(filepath.Join(f.CacheDir, "v2.0.3", "mitamae-armhf-linux")); err == nil {
		t.Error("the mismatching binary must not be cached")
	}
}

func TestFetchUnreachable(t *testing.T) {
	f := &mitamae.Fetcher{
		Version:   "2.0.3",
		Checksums: map[string]string{"x86_64": "00"},
		CacheDir:  t.TempDir(),
		BaseURL:   "http://127.0.0.1:0",
		Client:    http.DefaultClient,
	}
	if _, err := f.Fetch(context.Background(), "x86_64"); err == nil {
		t.Error("expected an error")
	}
}

func TestLocalArgs(t *testing.T) {
	nodes := []project.NodeFile{
		{Path: "a.json", Format: project.FormatJSON},
		{Path: "b.yml", Format: project.FormatYAML},
	}
	recipes := []string{"helpers/keeper.rb", "x/default.rb"}

	got := mitamae.LocalArgs(nodes, recipes, mitamae.Options{})
	want := []string{"local", "--node-json=a.json", "--node-yaml=b.yml", "helpers/keeper.rb", "x/default.rb"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}

	got = mitamae.LocalArgs(nil, recipes[:1], mitamae.Options{DryRun: true, LogLevel: "debug", NoColor: true})
	want = []string{"local", "--dry-run", "--log-level=debug", "--no-color", "helpers/keeper.rb"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}
