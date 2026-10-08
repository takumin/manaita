package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takumin/manaita/internal/config"
)

func TestFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	valid := write("valid.yml", "proxy: http://cache.internal|direct\nparallel: 8\nchdir:\n")

	cases := map[string]struct {
		path   string
		ok     bool
		values map[string]string
	}{
		"valid":         {valid, true, map[string]string{"proxy": "http://cache.internal|direct", "parallel": "8"}},
		"none":          {"", true, nil},
		"missing":       {filepath.Join(dir, "missing.yml"), false, nil},
		"empty":         {write("empty.yml", ""), true, nil},
		"unknown key":   {write("unknown.yml", "prxy: direct\n"), false, nil},
		"not a scalar":  {write("list.yml", "proxy: [direct]\n"), false, nil},
		"invalid yaml":  {write("invalid.yml", "proxy: [\n"), false, nil},
		"not a mapping": {write("scalar.yml", "direct\n"), false, nil},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			path := tt.path
			f := config.NewFile(&path)
			sources := map[string]interface{ Lookup() (string, bool) }{}
			for _, key := range []string{"proxy", "parallel", "chdir"} {
				s := f.Sources(key)
				sources[key] = &s
			}
			err := f.Load()
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error")
			}
			for key, s := range sources {
				got, ok := s.Lookup()
				if want, set := tt.values[key]; got != want || ok != set {
					t.Errorf("%s: want %q (%v), got %q (%v)", key, want, set, got, ok)
				}
			}
		})
	}
}

func TestFileSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("proxy: from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := config.NewFile(&path)
	sources := f.Sources("proxy", "TEST_MANAITA_PROXY")

	t.Setenv("TEST_MANAITA_PROXY", "from-env")
	if got, _ := sources.Lookup(); got != "from-env" {
		t.Errorf("want the environment first, got %q", got)
	}
	os.Unsetenv("TEST_MANAITA_PROXY") //nolint:errcheck,gosec
	if got, _ := sources.Lookup(); got != "from-file" {
		t.Errorf("want the file, got %q", got)
	}

	// The file is read again from the path it points to when it changes.
	other := filepath.Join(dir, "other.yml")
	if err := os.WriteFile(other, []byte("proxy: other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path = other
	if got, _ := sources.Lookup(); got != "other" {
		t.Errorf("want the other file, got %q", got)
	}
}

func TestDefaultFileMissing(t *testing.T) {
	if _, err := os.Stat(config.DefaultFile); err == nil {
		t.Skip(config.DefaultFile, "exists")
	}
	path := config.DefaultFile
	if err := config.NewFile(&path).Load(); err != nil {
		t.Errorf("a missing default file must be ignored: %v", err)
	}
}
