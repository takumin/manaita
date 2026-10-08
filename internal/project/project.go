// Package project loads a manaita project: the manifest at its root, the host
// inventory, the node attribute files and the recipes of each host.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FileName is the name of the manifest that marks the root of a project.
const FileName = "manaita.yml"

// LayerPlaceholder is replaced by the path of a layer in the hosts directory.
const LayerPlaceholder = "{layer}"

// Project is a mitamae repository described by its manifest.
type Project struct {
	// Root is the absolute path of the directory containing the manifest.
	Root string `yaml:"-"`

	Mitamae Mitamae `yaml:"mitamae"`
	// HostsDir is the directory of the host inventory. {layer} is replaced by
	// the path of each layer, which is appended to it when it has none, so
	// that the run lists can live next to the node files of the same layer.
	HostsDir string `yaml:"hosts"`
	// Nodes are the glob patterns of the node attribute files, from the least
	// to the most specific. {hostname} and {domain} are replaced by the values
	// of the host, and a pattern is skipped when one of them is empty.
	Nodes []string `yaml:"nodes"`
	// Prelude are the recipes run before the run list of every host.
	Prelude []string `yaml:"prelude"`
	// Plugins are fetched and given to mitamae with the plugin directory of
	// the project, verified against the hashes of the lock file.
	Plugins []Plugin `yaml:"plugins"`
	Remote  Remote   `yaml:"remote"`
}

// Mitamae pins the mitamae release used to apply the recipes.
type Mitamae struct {
	Version string `yaml:"version"`
	// Checksums are the SHA-256 digests of the release binaries by arch.
	Checksums map[string]string `yaml:"checksums"`
}

// Remote configures where the project is copied on the remote hosts.
type Remote struct {
	// Path is the destination directory, relative to the home directory of
	// the ssh user.
	Path string `yaml:"path"`
	// Exclude are rsync patterns of files not copied to the remote hosts.
	Exclude []string `yaml:"exclude"`
}

// Find returns the nearest directory containing the manifest, starting from
// dir and walking up to the filesystem root.
func Find(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", dir, err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
			return dir, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("failed to stat %s: %w", filepath.Join(dir, FileName), err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s not found in this directory or any parent", FileName)
		}
		dir = parent
	}
}

// Load reads the manifest in root.
func Load(root string) (*Project, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", root, err)
	}
	p := &Project{}
	if err := decodeFile(filepath.Join(root, FileName), p); err != nil {
		return nil, err
	}
	p.Root = root
	if p.HostsDir == "" {
		p.HostsDir = "hosts"
	}
	if p.Remote.Path == "" {
		p.Remote.Path = "mitamae"
	}
	if p.Mitamae.Version == "" {
		return nil, fmt.Errorf("%s: mitamae.version is required", FileName)
	}
	if strings.Count(p.HostsDir, LayerPlaceholder) > 1 {
		return nil, fmt.Errorf("%s: hosts must hold %s at most once", FileName, LayerPlaceholder)
	}
	if filepath.IsAbs(p.Remote.Path) {
		return nil, fmt.Errorf("%s: remote.path must be relative to the home directory", FileName)
	}
	if err := validatePlugins(p.Plugins); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return p, nil
}

func decodeFile(path string, v any) error {
	data, err := os.ReadFile(path) // #nosec G304 -- the files of the project are trusted
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return nil
}

// Open loads the project containing dir.
func Open(dir string) (*Project, error) {
	root, err := Find(dir)
	if err != nil {
		return nil, err
	}
	return Load(root)
}
