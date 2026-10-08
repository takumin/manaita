// Package mitamae fetches the mitamae release binaries and builds the
// command lines running them.
package mitamae

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/project"
)

// Arch returns the arch of the mitamae release binary for the output of
// `uname -m`.
func Arch(machine string) (string, error) {
	switch strings.TrimSpace(machine) {
	case "x86_64", "amd64":
		return "x86_64", nil
	case "aarch64", "arm64":
		return "aarch64", nil
	case "armv6l", "armv7l", "armhf":
		return "armhf", nil
	case "i386", "i486", "i586", "i686":
		return "i386", nil
	default:
		return "", fmt.Errorf("unsupported machine: %q", machine)
	}
}

// LocalArch returns the arch of the mitamae release binary for this machine.
func LocalArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	case "arm":
		return "armhf", nil
	case "386":
		return "i386", nil
	default:
		return "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}
}

// SystemCacheDir is the cache directory when neither the cache directory nor
// the user cache directory is set, as when run by cloud-init without HOME.
const SystemCacheDir = "/var/lib/cache/manaita"

// CacheDir returns the cache directory of manaita: dir when set, else manaita
// under the user cache directory, else SystemCacheDir.
func CacheDir(dir string) string {
	if dir != "" {
		return dir
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "manaita")
	}
	return SystemCacheDir
}

// ReleaseURL is the download URL of the mitamae releases, without the
// version. It is the origin, which a proxy list may replace by a cache server.
const ReleaseURL = "https://github.com/itamae-kitchen/mitamae/releases/download"

// Fetcher downloads the release binaries into a cache directory.
type Fetcher struct {
	Version   string
	Checksums map[string]string
	CacheDir  string
	// Command is the name of the installed binary looked up in PATH, used
	// instead of downloading when it matches the checksum. Empty disables
	// the lookup.
	Command string
	// BaseURL is the release download URL, without the version.
	BaseURL string
	// Downloader downloads the binaries, through the cache servers of its
	// proxy list.
	Downloader *fetch.Fetcher

	// mu serializes the downloads of hosts deployed in parallel.
	mu sync.Mutex
}

// NewFetcher returns a Fetcher of the mitamae release pinned by m, using the
// mitamae installed in PATH when it matches, and caching the binaries under
// the cache directory cacheDir otherwise, downloaded by dl.
func NewFetcher(m project.Mitamae, cacheDir string, dl *fetch.Fetcher) *Fetcher {
	return &Fetcher{
		Version:    m.Version,
		Checksums:  m.Checksums,
		CacheDir:   filepath.Join(CacheDir(cacheDir), "mitamae"),
		Command:    "mitamae",
		BaseURL:    ReleaseURL,
		Downloader: dl,
	}
}

// Fetch returns the path of the binary for arch, downloading it when it is
// not cached yet. The binary must match its pinned checksum.
func (f *Fetcher) Fetch(ctx context.Context, arch string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	want, err := f.checksum(arch)
	if err != nil {
		return "", err
	}
	if path, ok := f.installed(want); ok {
		return path, nil
	}
	path := f.Path(arch)
	if got, err := fileSHA256(path); err == nil && got == want {
		return path, nil
	}
	if f.Downloader == nil {
		return "", fmt.Errorf("no downloader for mitamae %s %s", f.Version, arch)
	}
	url := fmt.Sprintf("%s/v%s/mitamae-%s-linux", f.BaseURL, strings.TrimPrefix(f.Version, "v"), arch)
	// #nosec G302 -- the binary is executed
	if err := f.Downloader.File(ctx, url, path, want, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// Lookup returns the path of the binary Fetch would return for arch, without
// downloading: the installed binary when it matches, else the cached one,
// whether it is downloaded or not.
func (f *Fetcher) Lookup(arch string) string {
	if want, err := f.checksum(arch); err == nil {
		if path, ok := f.installed(want); ok {
			return path
		}
	}
	return f.Path(arch)
}

// checksum returns the SHA-256 pinned for arch, in lowercase hex.
func (f *Fetcher) checksum(arch string) (string, error) {
	want, ok := f.Checksums[arch]
	if !ok || want == "" {
		return "", fmt.Errorf("no checksum pinned for mitamae %s %s", f.Version, arch)
	}
	return strings.ToLower(strings.TrimPrefix(want, "sha256:")), nil
}

// installed returns the path of the binary found in PATH, when it matches
// the checksum want.
func (f *Fetcher) installed(want string) (string, bool) {
	if f.Command == "" {
		return "", false
	}
	path, err := exec.LookPath(f.Command)
	if err != nil {
		return "", false
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", false
	}
	if got, err := fileSHA256(path); err != nil || got != want {
		return "", false
	}
	return path, true
}

// Path returns the path of the cached binary for arch, whether it is
// downloaded or not.
func (f *Fetcher) Path(arch string) string {
	return filepath.Join(f.CacheDir, "v"+strings.TrimPrefix(f.Version, "v"), fmt.Sprintf("mitamae-%s-linux", arch))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- the path is in the cache directory or PATH
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Options are the options of `mitamae local`.
type Options struct {
	DryRun   bool
	LogLevel string
	NoColor  bool
}

// LocalArgs returns the arguments of `mitamae local` applying recipes with
// the node files.
func LocalArgs(nodes []project.NodeFile, recipes []string, opts Options) []string {
	args := []string{"local"}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	if opts.LogLevel != "" {
		args = append(args, "--log-level="+opts.LogLevel)
	}
	if opts.NoColor {
		args = append(args, "--no-color")
	}
	for _, n := range nodes {
		switch n.Format {
		case project.FormatJSON:
			args = append(args, "--node-json="+n.Path)
		case project.FormatYAML:
			args = append(args, "--node-yaml="+n.Path)
		}
	}
	return append(args, recipes...)
}
