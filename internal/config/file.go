package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
	"go.yaml.in/yaml/v3"
)

// DefaultFile is the configuration file of this machine read by default, as
// written by cloud-init or Ignition.
const DefaultFile = "/etc/manaita/config.yml"

// File is a YAML configuration file mapping keys to the values of flags. It
// is read lazily from the path it points to, so that the path can itself be
// set by a flag parsed before the flags it sets.
type File struct {
	path *string
	keys []string

	read   bool
	loaded string
	values map[string]any
	err    error
}

// NewFile returns the File read from *path.
func NewFile(path *string) *File {
	return &File{path: path}
}

// Sources returns the sources of a flag: the environment variables envs, then
// key of the file. key is registered as a known key.
func (f *File) Sources(key string, envs ...string) cli.ValueSourceChain {
	if !slices.Contains(f.keys, key) {
		f.keys = append(f.keys, key)
	}
	sources := cli.EnvVars(envs...)
	sources.Chain = append(sources.Chain, &fileSource{file: f, key: key})
	return sources
}

// Load reads the file, or nothing when the path is empty or is DefaultFile
// and does not exist. Unknown keys and values other than scalars are errors.
func (f *File) Load() error {
	path := *f.path
	if f.read && f.loaded == path {
		return f.err
	}
	f.read, f.loaded, f.values, f.err = true, path, nil, nil
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the path is given by the user
	if errors.Is(err, fs.ErrNotExist) && path == DefaultFile {
		return nil
	}
	if err != nil {
		f.err = fmt.Errorf("failed to read the configuration file: %w", err)
		return f.err
	}
	var values map[string]any
	if err := yaml.Unmarshal(data, &values); err != nil {
		f.err = fmt.Errorf("failed to parse %s: %w", path, err)
		return f.err
	}
	for key, value := range values {
		if !slices.Contains(f.keys, key) {
			f.err = fmt.Errorf("%s: unknown key %q, expected one of %s", path, key, strings.Join(f.keys, ", "))
			return f.err
		}
		switch value.(type) {
		case string, int, float64, bool, nil:
		default:
			f.err = fmt.Errorf("%s: %s must be a scalar", path, key)
			return f.err
		}
	}
	f.values = values
	return nil
}

// fileSource is the value of a key of a File.
type fileSource struct {
	file *File
	key  string
}

// Lookup returns the value of the key. A file failing to load has no values:
// its error is reported by Load.
func (s *fileSource) Lookup() (string, bool) {
	if s.file.Load() != nil {
		return "", false
	}
	v, ok := s.file.values[s.key]
	if !ok || v == nil {
		return "", false
	}
	return fmt.Sprint(v), true
}

func (s *fileSource) String() string {
	return fmt.Sprintf("key %q of file %q", s.key, *s.file.path)
}

func (s *fileSource) GoString() string {
	return fmt.Sprintf("&fileSource{key:%q, path:%q}", s.key, *s.file.path)
}
