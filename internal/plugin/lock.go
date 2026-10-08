package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/takumin/manaita/internal/logging"
	"github.com/takumin/manaita/internal/project"
)

// LockFileName is the lock file next to the manifest, holding the hashes of
// the plugins.
const LockFileName = "manaita.lock"

// Lock maps the plugins, by their repository and commit, to their h1 hashes.
// Each line of the lock file is "<repo> <rev> <hash>".
type Lock map[project.Plugin]string

// ReadLock reads the lock file of the project root root. A missing file is an
// empty lock.
func ReadLock(root string) (Lock, error) {
	path := filepath.Join(root, LockFileName)
	data, err := os.ReadFile(path) // #nosec G304 -- the files of the project are trusted
	if errors.Is(err, os.ErrNotExist) {
		return Lock{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	lock := Lock{}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.HasPrefix(fields[2], "h1:") {
			return nil, fmt.Errorf("%s:%d: want \"<repo> <rev> h1:<hash>\", got %q", LockFileName, i+1, line)
		}
		lock[project.Plugin{Repo: fields[0], Rev: fields[1]}] = fields[2]
	}
	return lock, nil
}

// Write writes the lock file of the project root root, sorted by repository.
// An empty lock removes the file.
func (l Lock) Write(root string) error {
	path := filepath.Join(root, LockFileName)
	if len(l) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	plugins := make([]project.Plugin, 0, len(l))
	for pl := range l {
		plugins = append(plugins, pl)
	}
	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Repo != plugins[j].Repo {
			return plugins[i].Repo < plugins[j].Repo
		}
		return plugins[i].Rev < plugins[j].Rev
	})
	var b strings.Builder
	for _, pl := range plugins {
		fmt.Fprintf(&b, "%s %s %s\n", pl.Repo, pl.Rev, l[pl])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644) // #nosec G306 -- the lock file is committed with the project
}

// Hash returns the hash of pl, or an error telling to update the lock file
// when it has none.
func (l Lock) Hash(pl project.Plugin) (string, error) {
	hash, ok := l[pl]
	if !ok {
		return "", fmt.Errorf("plugin %s is not in %s: run manaita lock", pl, LockFileName)
	}
	return hash, nil
}

// Update downloads every plugin of p with store and writes their hashes to
// the lock file of p, which then holds only the plugins of p.
func Update(ctx context.Context, p *project.Project, store *Store) error {
	lock := Lock{}
	for _, pl := range p.Plugins {
		hash, err := store.Download(ctx, pl)
		if err != nil {
			return err
		}
		logging.FromContext(ctx).InfoContext(ctx, "locked the plugin", slog.String("plugin", pl.String()), slog.String("hash", hash))
		lock[pl] = hash
	}
	return lock.Write(p.Root)
}
