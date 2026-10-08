package project

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// PluginsDir is the plugin directory of mitamae, relative to the project root.
const PluginsDir = "plugins"

// Plugin is a mitamae plugin fetched from its repository, pinned to a commit.
type Plugin struct {
	// Repo is the repository, like github.com/owner/itamae-plugin-recipe-apt.
	Repo string `yaml:"repo"`
	// Rev is the full SHA-1 of the commit.
	Rev string `yaml:"rev"`
}

var (
	pluginRepo = regexp.MustCompile(`^github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	pluginName = regexp.MustCompile(`^m?itamae-plugin-(recipe|resource)-(.+)$`)
	pluginRev  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// pluginRecipe is a recipe of a plugin, as named by include_recipe.
	pluginRecipe = regexp.MustCompile(`^[A-Za-z0-9_-]+(::[A-Za-z0-9_-]+)*$`)
)

// Name returns the name of the repository, which is the directory of the
// plugin given to mitamae.
func (pl Plugin) Name() string {
	return path.Base(pl.Repo)
}

// String returns the repository and the commit.
func (pl Plugin) String() string {
	return pl.Repo + "@" + pl.Rev
}

func (pl Plugin) validate() error {
	if !pluginRepo.MatchString(pl.Repo) || strings.Contains(pl.Repo, "/.") {
		return fmt.Errorf("invalid plugin repository %q: want github.com/<owner>/<name>", pl.Repo)
	}
	if !pluginName.MatchString(pl.Name()) {
		return fmt.Errorf("invalid plugin repository %q: the name must start with itamae-plugin-recipe-, itamae-plugin-resource- or their mitamae- forms", pl.Repo)
	}
	if !pluginRev.MatchString(pl.Rev) {
		return fmt.Errorf("invalid rev of plugin %s: %q is not a full commit SHA in lowercase", pl.Repo, pl.Rev)
	}
	return nil
}

// validatePlugins checks the plugins of the manifest, whose names must be
// unique since they are the directories given to mitamae.
func validatePlugins(plugins []Plugin) error {
	names := map[string]string{}
	for _, pl := range plugins {
		if err := pl.validate(); err != nil {
			return err
		}
		if other, ok := names[pl.Name()]; ok {
			return fmt.Errorf("plugins %s and %s have the same name", other, pl.Repo)
		}
		names[pl.Name()] = pl.Repo
	}
	return nil
}

// LocalPlugins returns the names of the plugin directories of the project, in
// the plugin directory of mitamae, sorted.
func (p *Project) LocalPlugins() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(p.Root, PluginsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the plugins: %w", err)
	}
	names := []string{}
	for _, e := range entries {
		if hidden(e.Name()) {
			continue
		}
		if info, err := os.Stat(filepath.Join(p.Root, PluginsDir, e.Name())); err == nil && info.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// recipePlugin reports whether a recipe plugin named name, the first part of
// a plugin recipe, is declared by the manifest or in the plugin directory.
func (p *Project) recipePlugin(name string) (bool, error) {
	candidates := []string{"itamae-plugin-recipe-" + name, "mitamae-plugin-recipe-" + name}
	for _, pl := range p.Plugins {
		if slices.Contains(candidates, pl.Name()) {
			return true, nil
		}
	}
	local, err := p.LocalPlugins()
	if err != nil {
		return false, err
	}
	for _, c := range candidates {
		if slices.Contains(local, c) {
			return true, nil
		}
	}
	return false, nil
}

// PluginRecipeFiles returns the candidate files of the plugin recipe name in
// the directory of its plugin, as mitamae looks them up: apt is
// mrblib/itamae/plugin/recipe/apt/default.rb or .../apt.rb, and apt::source
// is .../apt/source.rb, under mrblib/itamae or mrblib/mitamae.
func PluginRecipeFiles(name string) []string {
	parts := strings.Split(name, "::")
	var rels []string
	if len(parts) == 1 {
		rels = []string{path.Join(parts[0], "default.rb"), parts[0] + ".rb"}
	} else {
		rels = []string{path.Join(parts...) + ".rb"}
	}
	var files []string
	for _, ns := range []string{"itamae", "mitamae"} {
		for _, rel := range rels {
			files = append(files, path.Join("mrblib", ns, "plugin", "recipe", rel))
		}
	}
	return files
}

// PluginOf returns the first part of the plugin recipe name, which names its
// recipe plugin.
func PluginOf(name string) string {
	first, _, _ := strings.Cut(name, "::")
	return first
}
