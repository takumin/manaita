// Package mitamae fetches the mitamae release binaries and builds the
// command lines running them.
package mitamae

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

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

// CacheDirEnv is the environment variable setting the cache directory.
const CacheDirEnv = "MANAITA_CACHE_DIR"

// SystemCacheDir is the cache directory when neither CacheDirEnv nor the user
// cache directory is set, as when run by cloud-init without HOME.
const SystemCacheDir = "/var/lib/cache/manaita"

// CacheDir returns the cache directory of manaita: CacheDirEnv when set,
// else manaita under the user cache directory, else SystemCacheDir.
func CacheDir() string {
	if dir := os.Getenv(CacheDirEnv); dir != "" {
		return dir
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "manaita")
	}
	return SystemCacheDir
}

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
	Client  *http.Client

	// mu serializes the downloads of hosts deployed in parallel.
	mu sync.Mutex
}

// NewFetcher returns a Fetcher of the mitamae release pinned by m, using the
// mitamae installed in PATH when it matches, and caching the binaries under
// CacheDir otherwise.
func NewFetcher(m project.Mitamae) *Fetcher {
	return &Fetcher{
		Version:   m.Version,
		Checksums: m.Checksums,
		CacheDir:  filepath.Join(CacheDir(), "mitamae"),
		Command:   "mitamae",
		BaseURL:   "https://github.com/itamae-kitchen/mitamae/releases/download",
		Client:    http.DefaultClient,
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
	asset := fmt.Sprintf("mitamae-%s-linux", arch)
	path := f.Path(arch)

	if got, err := fileSHA256(path); err == nil && got == want {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("failed to create the cache directory: %w", err)
	}

	url := fmt.Sprintf("%s/v%s/%s", f.BaseURL, strings.TrimPrefix(f.Version, "v"), asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := f.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: %s", url, res.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), asset+".*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), res.Body); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("checksum mismatch for %s: want %s, got %s", url, want, got)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil { // #nosec G302 -- the binary is executed
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
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
