package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Node file formats, as passed to mitamae.
const (
	FormatJSON = "json"
	FormatYAML = "yaml"
)

// NodeFile is a node attribute file of a host.
type NodeFile struct {
	// Path is relative to the project root.
	Path   string
	Format string
}

var placeholder = regexp.MustCompile(`\{([a-z]+)\}`)

// NodeFiles returns the node attribute files of h in the order of the
// patterns. A file matched by several patterns, directly or through a
// symlink, is only returned the first time.
func (p *Project) NodeFiles(h *Host) ([]NodeFile, error) {
	vars := map[string]string{
		"hostname": h.Name,
		"domain":   h.Domain,
	}
	files := []NodeFile{}
	seen := map[string]bool{}
	for _, pattern := range p.Nodes {
		expanded, ok, err := expand(pattern, vars)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(p.Root, expanded))
		if err != nil {
			return nil, fmt.Errorf("invalid node pattern %q: %w", pattern, err)
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				return nil, fmt.Errorf("failed to stat %s: %w", m, err)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			real, err := filepath.EvalSymlinks(m)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve %s: %w", m, err)
			}
			if seen[real] {
				continue
			}
			seen[real] = true
			rel, err := filepath.Rel(p.Root, m)
			if err != nil {
				return nil, err
			}
			format, err := nodeFormat(rel)
			if err != nil {
				return nil, err
			}
			files = append(files, NodeFile{Path: filepath.ToSlash(rel), Format: format})
		}
	}
	return files, nil
}

// expand replaces the placeholders of pattern. It reports false when a
// placeholder has an empty value, so that the pattern is skipped.
func expand(pattern string, vars map[string]string) (string, bool, error) {
	ok := true
	var err error
	expanded := placeholder.ReplaceAllStringFunc(pattern, func(s string) string {
		name := s[1 : len(s)-1]
		v, known := vars[name]
		switch {
		case !known:
			err = fmt.Errorf("unknown placeholder %s in node pattern %q", s, pattern)
		case v == "":
			ok = false
		case strings.ContainsAny(v, `/\*?[`):
			err = fmt.Errorf("invalid %s: %q", name, v)
		}
		return v
	})
	if err != nil {
		return "", false, err
	}
	return expanded, ok, nil
}

func nodeFormat(path string) (string, error) {
	switch filepath.Ext(path) {
	case ".json":
		return FormatJSON, nil
	case ".yml", ".yaml":
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("unsupported node file: %s", path)
	}
}

// Recipe is a recipe of a run list: a file of the project or a recipe of a
// plugin.
type Recipe struct {
	// Path is the file, relative to the project root, empty for a plugin
	// recipe.
	Path string
	// Plugin is the name of the plugin recipe, like apt or apt::source, as
	// given to include_recipe.
	Plugin string
}

// String returns the file of the recipe, or the name of the plugin recipe.
func (r Recipe) String() string {
	if r.Plugin != "" {
		return r.Plugin
	}
	return r.Path
}

// ResolveRecipe returns the recipe name. name is a file of the project, a
// file without the .rb extension, or a directory containing default.rb. When
// there is no such file, name is a recipe of a plugin, like apt or
// apt::source, when its recipe plugin is declared by the manifest or in the
// plugin directory of the project.
func (p *Project) ResolveRecipe(name string) (Recipe, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return Recipe{}, fmt.Errorf("recipe %s is outside of the project", name)
	}
	for _, candidate := range []string{
		clean,
		clean + ".rb",
		filepath.Join(clean, "default.rb"),
	} {
		info, err := os.Stat(filepath.Join(p.Root, candidate))
		if err == nil && info.Mode().IsRegular() {
			return Recipe{Path: filepath.ToSlash(candidate)}, nil
		}
	}
	if pluginRecipe.MatchString(name) {
		ok, err := p.recipePlugin(PluginOf(name))
		if err != nil {
			return Recipe{}, err
		}
		if ok {
			return Recipe{Plugin: name}, nil
		}
	}
	return Recipe{}, fmt.Errorf("recipe %s not found", name)
}

// ResolveRecipes resolves every recipe of names.
func (p *Project) ResolveRecipes(names []string) ([]Recipe, error) {
	recipes := make([]Recipe, 0, len(names))
	for _, name := range names {
		r, err := p.ResolveRecipe(name)
		if err != nil {
			return nil, err
		}
		recipes = append(recipes, r)
	}
	return recipes, nil
}
