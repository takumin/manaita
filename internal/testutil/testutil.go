// Package testutil builds project fixtures for the tests.
package testutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// WriteFiles creates files under root, keyed by their slash-separated path.
func WriteFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Manifest is a manifest exercising every node layer.
//
//go:embed testdata/manaita.yml
var Manifest string

// Project creates a project fixture and returns its root.
func Project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	WriteFiles(t, root, map[string]string{
		"manaita.yml":                                    Manifest,
		"helpers/keeper.rb":                              "",
		"cookbooks/common/sudo/default.rb":               "",
		"cookbooks/server/dnsmasq/default.rb":            "",
		"cookbooks/server/dnsmasq/extra.rb":              "",
		"hosts/README.md":                                "",
		"hosts/all/common.yml":                           "",
		"hosts/domains/example.internal/common.yml":      "run_list:\n  - cookbooks/common/sudo\n",
		"hosts/hosts/dsk/run_list.yml":                   "run_list:\n  - cookbooks/common/sudo\n",
		"hosts/hosts/rpi4/run_list.yml":                  "run_list:\n  - cookbooks/common/sudo\n",
		"hosts/hosts/rpi4/z.yaml":                        "",
		"hosts/hosts/empty/host.yml":                     "",
		"hosts/hosts/empty/README.md":                    "",
		"hosts/hosts/.hidden/host.yml":                   "",
		"hosts/hosts/nofiles/README.md":                  "",
		"hosts/fqdns/example.internal/rpi4/run_list.yml": "run_list:\n  - cookbooks/server/dnsmasq/extra\n",
		"hosts/fqdns/other.internal/rpi4/run_list.yml":   "run_list:\n  - cookbooks/server/dnsmasq\n",
		"hosts/fqdns/.hidden/dsk/host.yml":               "",
		"nodes/all/b.yml":                                "",
		"nodes/all/a.yml":                                "",
		"nodes/all/c.json":                               "{}",
		"nodes/all/ignored.txt":                          "",
		"nodes/domains/example.internal/d.yml":           "",
		"nodes/domains/example.internal/inventory/x.yml": "",
		"nodes/hosts/dsk/h.yml":                          "",
		"nodes/hosts/rpi4/h.yml":                         "",
		"nodes/fqdns/example.internal/rpi4/f.yml":        "",
	})
	link := filepath.Join(root, "nodes/hosts/rpi4/inventory")
	if err := os.Symlink("../../domains/example.internal/inventory", link); err != nil {
		t.Fatal(err)
	}
	return root
}

// Archive returns a tar.gz archive of files, keyed by their slash-separated
// path, under the top directory top, like the archives of GitHub. An empty
// content with a path ending in a slash is a directory.
func Archive(t *testing.T, top string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	headers := []*tar.Header{
		{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "0123"}},
		{Typeflag: tar.TypeDir, Name: top + "/", Mode: 0o775},
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.HasSuffix(name, "/") {
			headers = append(headers, &tar.Header{Typeflag: tar.TypeDir, Name: top + "/" + name, Mode: 0o775})
			continue
		}
		headers = append(headers, &tar.Header{Typeflag: tar.TypeReg, Name: top + "/" + name, Mode: 0o664, Size: int64(len(files[name]))})
	}
	for _, h := range headers {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(files[strings.TrimPrefix(h.Name, top+"/")])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
