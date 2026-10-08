package plugin_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/plugin"
	"github.com/takumin/manaita/internal/project"
	"github.com/takumin/manaita/internal/testutil"
)

const rev = "0123456789abcdef0123456789abcdef01234567"

var apt = project.Plugin{Repo: "github.com/owner/itamae-plugin-recipe-apt", Rev: rev}

// files are the files of the apt plugin, whose hash is aptHash.
var files = map[string]string{
	"README.md": "apt\n",
	"mrblib/":   "",
	"mrblib/itamae/plugin/recipe/apt/default.rb": "package 'apt'\n",
}

func TestHash(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteFiles(t, dir, map[string]string{"a.rb": "a\n", "sub/b.rb": "bb"})
	got, err := plugin.Hash(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The hash of golang.org/x/mod/sumdb/dirhash.HashDir(dir, "", Hash1).
	if want := "h1:GOOcJxBYLPdLLr6Ue+ln+7S77dHSrqtmybDfOCIYiuA="; got != want {
		t.Errorf("want %s, got %s", want, got)
	}

	if err := os.Chmod(filepath.Join(dir, "a.rb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if again, err := plugin.Hash(dir); err != nil || again != got {
		t.Errorf("the mode must not change the hash: %s (%v)", again, err)
	}

	if _, err := plugin.Hash(t.TempDir()); err == nil {
		t.Error("expected an empty directory error")
	}
	if _, err := plugin.Hash(filepath.Join(dir, "missing")); err == nil {
		t.Error("expected a missing directory error")
	}
	if err := os.Symlink("a.rb", filepath.Join(dir, "link.rb")); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Hash(dir); err == nil {
		t.Error("expected a symlink error")
	}
}

// server serves the archive of apt at its codeload path, counting the
// requests.
func server(t *testing.T, archive []byte) (*plugin.Store, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/owner/itamae-plugin-recipe-apt/tar.gz/"+rev {
			http.NotFound(w, r)
			return
		}
		w.Write(archive) //nolint:errcheck,gosec
	}))
	t.Cleanup(srv.Close)
	dl, err := fetch.New(fetch.Direct)
	if err != nil {
		t.Fatal(err)
	}
	s := plugin.NewStore(t.TempDir(), dl)
	s.BaseURL = srv.URL
	return s, &requests
}

func aptHash(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFiles(t, dir, map[string]string{
		"README.md": files["README.md"],
		"mrblib/itamae/plugin/recipe/apt/default.rb": files["mrblib/itamae/plugin/recipe/apt/default.rb"],
	})
	hash, err := plugin.Hash(dir)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestStore(t *testing.T) {
	s, requests := server(t, testutil.Archive(t, "itamae-plugin-recipe-apt-"+rev, files))
	if want := "https://codeload.github.com/owner/itamae-plugin-recipe-apt/tar.gz/" + rev; plugin.NewStore("", nil).URL(apt) != want {
		t.Errorf("want URL %s, got %s", want, plugin.NewStore("", nil).URL(apt))
	}
	hash := aptHash(t)
	ctx := context.Background()

	dir, err := s.Fetch(ctx, apt, hash)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(s.Dir, "github.com", "owner", "itamae-plugin-recipe-apt", rev) {
		t.Errorf("unexpected directory: %s", dir)
	}
	got, err := os.ReadFile(filepath.Join(dir, "mrblib/itamae/plugin/recipe/apt/default.rb"))
	if err != nil || string(got) != "package 'apt'\n" {
		t.Errorf("unexpected recipe: %q (%v)", got, err)
	}

	if _, err := s.Fetch(ctx, apt, hash); err != nil || requests.Load() != 1 {
		t.Errorf("a cached plugin is not downloaded again: %d requests (%v)", requests.Load(), err)
	}

	// A modified cache is downloaded again.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fetch(ctx, apt, hash); err != nil || requests.Load() != 2 {
		t.Errorf("a modified plugin is downloaded again: %d requests (%v)", requests.Load(), err)
	}
	if got, err := plugin.Hash(dir); err != nil || got != hash {
		t.Errorf("the cache must be restored: %s (%v)", got, err)
	}

	// A mismatching download keeps the cache as it is.
	if _, err := s.Fetch(ctx, apt, "h1:other"); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("expected a hash mismatch, got %v", err)
	}
	if got, err := plugin.Hash(dir); err != nil || got != hash {
		t.Errorf("the cache must be kept: %s (%v)", got, err)
	}

	if got, err := s.Download(ctx, apt); err != nil || got != hash || requests.Load() != 4 {
		t.Errorf("Download downloads again: %s, %d requests (%v)", got, requests.Load(), err)
	}

	missing := project.Plugin{Repo: "github.com/owner/itamae-plugin-recipe-missing", Rev: rev}
	if _, err := s.Fetch(ctx, missing, hash); err == nil {
		t.Error("expected a not found error")
	}
	if _, err := plugin.NewStore(t.TempDir(), nil).Fetch(ctx, apt, hash); err == nil {
		t.Error("expected a missing downloader error")
	}
}

func TestStoreInvalidArchives(t *testing.T) {
	entry := func(h *tar.Header, body string) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, hdr := range []*tar.Header{{Typeflag: tar.TypeDir, Name: "top/", Mode: 0o755}, h} {
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	cases := map[string][]byte{
		"not gzip":  []byte("plain"),
		"symlink":   entry(&tar.Header{Typeflag: tar.TypeSymlink, Name: "top/link", Linkname: "/etc/passwd"}, ""),
		"traversal": entry(&tar.Header{Typeflag: tar.TypeReg, Name: "top/../../x", Size: 1, Mode: 0o644}, "x"),
		"absolute":  entry(&tar.Header{Typeflag: tar.TypeReg, Name: "top//etc/x", Size: 1, Mode: 0o644}, "x"),
		"empty":     testutil.Archive(t, "top", nil),
	}
	for name, archive := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := server(t, archive)
			if _, err := s.Download(context.Background(), apt); err == nil {
				t.Error("expected an error")
			}
			if entries, _ := os.ReadDir(filepath.Dir(s.Path(apt))); len(entries) != 0 {
				t.Errorf("nothing must be left in the cache: %v", entries)
			}
		})
	}
}

func TestStoreExecutable(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "top/bin/run", Size: 2, Mode: 0o775}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("#!")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ := server(t, buf.Bytes())
	if _, err := s.Download(context.Background(), apt); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(s.Path(apt), "bin", "run"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("want an executable file, got %v (%v)", info.Mode(), err)
	}
}

func TestLock(t *testing.T) {
	root := t.TempDir()
	lock, err := plugin.ReadLock(root)
	if err != nil || len(lock) != 0 {
		t.Fatalf("a missing lock file is empty: %v (%v)", lock, err)
	}
	if _, err := lock.Hash(apt); err == nil || !strings.Contains(err.Error(), "manaita lock") {
		t.Errorf("expected an error telling to lock, got %v", err)
	}

	other := project.Plugin{Repo: "github.com/owner/itamae-plugin-resource-a", Rev: rev}
	lock = plugin.Lock{apt: "h1:apt", other: "h1:a"}
	if err := lock.Write(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, plugin.LockFileName))
	if err != nil {
		t.Fatal(err)
	}
	want := "github.com/owner/itamae-plugin-recipe-apt " + rev + " h1:apt\n" +
		"github.com/owner/itamae-plugin-resource-a " + rev + " h1:a\n"
	if string(data) != want {
		t.Errorf("want\n%s\ngot\n%s", want, data)
	}
	read, err := plugin.ReadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if hash, err := read.Hash(apt); err != nil || hash != "h1:apt" {
		t.Errorf("unexpected hash: %s (%v)", hash, err)
	}

	if err := (plugin.Lock{}).Write(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, plugin.LockFileName)); !os.IsNotExist(err) {
		t.Errorf("an empty lock removes the file: %v", err)
	}
	if err := (plugin.Lock{}).Write(root); err != nil {
		t.Errorf("removing a missing lock file: %v", err)
	}

	for _, content := range []string{"github.com/owner/x " + rev + "\n", "a b sha256:x\n"} {
		testutil.WriteFiles(t, root, map[string]string{plugin.LockFileName: content})
		if _, err := plugin.ReadLock(root); err == nil {
			t.Errorf("%q: expected an error", content)
		}
	}
}

func TestUpdate(t *testing.T) {
	s, _ := server(t, testutil.Archive(t, "top", files))
	root := t.TempDir()
	testutil.WriteFiles(t, root, map[string]string{
		project.FileName:    "mitamae:\n  version: 2.0.3\nplugins:\n  - repo: " + apt.Repo + "\n    rev: " + rev + "\n",
		plugin.LockFileName: "github.com/owner/stale " + rev + " h1:x\n",
	})
	p, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.Update(context.Background(), p, s); err != nil {
		t.Fatal(err)
	}
	lock, err := plugin.ReadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock) != 1 || lock[apt] != aptHash(t) {
		t.Errorf("unexpected lock: %v", lock)
	}

	p.Plugins = append(p.Plugins, project.Plugin{Repo: "github.com/owner/itamae-plugin-recipe-missing", Rev: rev})
	if err := plugin.Update(context.Background(), p, s); err == nil {
		t.Error("expected a download error")
	}
}
