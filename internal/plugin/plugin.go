// Package plugin fetches the mitamae plugins of a project into a cache
// directory, verifying them against the hashes of the lock file.
//
// A plugin is downloaded as the archive of its commit, possibly through a
// cache server, and extracted. The archive of a commit has changed when
// GitHub changed how it builds them, so the hash is computed from the
// extracted files, like the h1 hashes of go.sum, instead of the archive.
package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/project"
)

// ArchiveURL is the origin of the archives of the commits. A proxy list may
// replace it by a cache server.
const ArchiveURL = "https://codeload.github.com"

// Store fetches the plugins into a cache directory.
type Store struct {
	// Dir holds the plugins at <repo>/<rev>.
	Dir string
	// BaseURL is the origin of the archives.
	BaseURL string
	// Downloader downloads the archives, through the cache servers of its
	// proxy list.
	Downloader *fetch.Fetcher

	// mu serializes the downloads of hosts deployed in parallel.
	mu sync.Mutex
}

// NewStore returns a Store caching the plugins under the cache directory
// cacheDir, downloaded by dl.
func NewStore(cacheDir string, dl *fetch.Fetcher) *Store {
	return &Store{
		Dir:        filepath.Join(cacheDir, "plugins"),
		BaseURL:    ArchiveURL,
		Downloader: dl,
	}
}

// Path returns the directory of pl in the cache, whether it is fetched or
// not.
func (s *Store) Path(pl project.Plugin) string {
	return filepath.Join(s.Dir, filepath.FromSlash(pl.Repo), pl.Rev)
}

// URL returns the URL of the archive of pl.
func (s *Store) URL(pl project.Plugin) string {
	owner := filepath.Base(filepath.Dir(filepath.FromSlash(pl.Repo)))
	return fmt.Sprintf("%s/%s/%s/tar.gz/%s", strings.TrimSuffix(s.BaseURL, "/"), owner, pl.Name(), pl.Rev)
}

// Fetch returns the directory of pl, downloading it when it is not cached
// yet. The plugin must match hash, its h1 hash from the lock file; a cached
// plugin not matching it is downloaded again.
func (s *Store) Fetch(ctx context.Context, pl project.Plugin, hash string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.Path(pl)
	if got, err := Hash(dir); err == nil && got == hash {
		return dir, nil
	}
	_, err := s.download(ctx, pl, func(got string) error {
		if got != hash {
			return fmt.Errorf("hash mismatch for plugin %s: want %s, got %s", pl, hash, got)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// Download downloads pl again into the cache, whether it is cached or not,
// and returns its h1 hash.
func (s *Store) Download(ctx context.Context, pl project.Plugin) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.download(ctx, pl, func(string) error { return nil })
}

// download downloads and extracts pl into the cache, replacing the cached
// directory once verify accepts its hash.
func (s *Store) download(ctx context.Context, pl project.Plugin, verify func(hash string) error) (string, error) {
	if s.Downloader == nil {
		return "", fmt.Errorf("no downloader for plugin %s", pl)
	}
	dir := s.Path(pl)
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil { // #nosec G301 -- mitamae reads the plugins as root
		return "", fmt.Errorf("failed to create %s: %w", parent, err)
	}
	var hash string
	err := s.Downloader.Get(ctx, s.URL(pl), func(target string, body io.Reader) error {
		tmp, err := os.MkdirTemp(parent, pl.Rev+".*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp) //nolint:errcheck
		// #nosec G302 -- mitamae reads the plugins as root
		if err := os.Chmod(tmp, 0o755); err != nil {
			return err
		}
		if err := extract(body, tmp); err != nil {
			return fmt.Errorf("failed to extract %s: %w", target, err)
		}
		if hash, err = Hash(tmp); err != nil {
			return err
		}
		if err := verify(hash); err != nil {
			return fmt.Errorf("%s: %w", target, err)
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		return os.Rename(tmp, dir)
	})
	if err != nil {
		return "", err
	}
	return hash, nil
}

// extract extracts the tar.gz archive r into dir, without the top directory
// of the archive. Only directories and regular files are allowed.
func extract(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close() //nolint:errcheck
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return gz.Close()
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		_, rel, _ := strings.Cut(strings.TrimPrefix(hdr.Name, "./"), "/")
		rel = strings.TrimSuffix(rel, "/")
		if rel == "" {
			continue
		}
		if !filepath.IsLocal(rel) || strings.ContainsAny(rel, "\\\n") {
			return fmt.Errorf("invalid path in the archive: %q", hdr.Name)
		}
		path := filepath.Join(dir, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil { // #nosec G301 -- mitamae reads the plugins as root
				return err
			}
		case tar.TypeReg:
			if err := writeFile(path, tr, hdr.FileInfo().Mode()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported entry in the archive: %q is not a directory or a regular file", hdr.Name)
		}
	}
}

// writeFile writes r to the new file path, executable when mode is.
func writeFile(path string, r io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- mitamae reads the plugins as root
		return err
	}
	perm := fs.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) // #nosec G304 -- the path is checked to be local
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil { // #nosec G110 -- the archive is verified by its hash once extracted
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Hash returns the h1 hash of the files under dir, as go.sum hashes the
// modules: the SHA-256 of the lines "<SHA-256 of the file in hex>  <path>",
// sorted by path, encoded in base64 after "h1:". The file modes are not part
// of the hash. Only directories and regular files are allowed.
func Hash(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			return fmt.Errorf("unexpected file %s: not a directory or a regular file", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no file in %s", dir)
	}
	sort.Strings(files)
	summary := sha256.New()
	for _, name := range files {
		sum, err := fileSHA256(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(summary, "%x  %s\n", sum, name) //nolint:errcheck // a hash never fails to write
	}
	return "h1:" + base64.StdEncoding.EncodeToString(summary.Sum(nil)), nil
}

func fileSHA256(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- the path is in the cache directory
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
