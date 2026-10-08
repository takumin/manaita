package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/project"
)

// The directories of a stage.
const (
	// stagePlugins holds the plugin directory given to mitamae.
	stagePlugins = "plugins"
	// stageRecipes holds the recipes including the plugin recipes.
	stageRecipes = "recipes"
)

// LocalStageDir returns the stage directory of the project root root on this
// machine, under the cache directory cacheDir.
func LocalStageDir(cacheDir, root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(mitamae.CacheDir(cacheDir), "stage", hex.EncodeToString(sum[:8]))
}

// staged reports whether plan needs a stage: the project declares plugins,
// or plan has plugin recipes.
func (d *Deployer) staged(plan *Plan) bool {
	if len(d.Project.Plugins) > 0 {
		return true
	}
	for _, r := range plan.Recipes {
		if r.Plugin != "" {
			return true
		}
	}
	return false
}

// args returns the arguments of `mitamae local` applying plan, with the
// stage directory stage as seen by mitamae.
func (d *Deployer) args(plan *Plan, stage string) []string {
	opts := d.Options
	if len(d.Project.Plugins) > 0 {
		opts.Plugins = path.Join(filepath.ToSlash(stage), stagePlugins)
	}
	recipes := make([]string, len(plan.Recipes))
	for i, r := range plan.Recipes {
		recipes[i] = r.Path
		if r.Plugin != "" {
			recipes[i] = path.Join(filepath.ToSlash(stage), stageRecipes, includeFile(r.Plugin))
		}
	}
	return mitamae.LocalArgs(plan.Nodes, recipes, opts)
}

// includeFile returns the name of the recipe including the plugin recipe
// name. It holds dots, which the names of the plugin recipes cannot, so that
// include_recipe never finds it instead of the plugin recipe.
func includeFile(name string) string {
	return "include." + strings.ReplaceAll(name, "::", ".") + ".rb"
}

// stage creates the stage directory dir of plan. When the project declares
// plugins, the plugin directory links to them, fetched and verified, and to
// the plugins of the project, since mitamae reads a single plugin directory.
// The recipes directory holds a recipe including each plugin recipe of plan,
// which must be found in its plugin.
func (d *Deployer) stage(ctx context.Context, plan *Plan, dir string) error {
	plugins := filepath.Join(d.Project.Root, project.PluginsDir)
	if len(d.Project.Plugins) > 0 {
		plugins = filepath.Join(dir, stagePlugins)
		if err := d.stagePlugins(ctx, plugins); err != nil {
			return err
		}
	}
	recipes := filepath.Join(dir, stageRecipes)
	if err := os.MkdirAll(recipes, 0o755); err != nil { // #nosec G301 -- mitamae reads the stage as root
		return err
	}
	for _, r := range plan.Recipes {
		if r.Plugin == "" {
			continue
		}
		if !pluginRecipeExists(plugins, r.Plugin) {
			return fmt.Errorf("host %s: plugin recipe %s not found in its plugin", plan.Host.FQDN(), r.Plugin)
		}
		content := fmt.Sprintf("include_recipe '%s'\n", r.Plugin)
		// #nosec G306 -- mitamae reads the stage as root
		if err := os.WriteFile(filepath.Join(recipes, includeFile(r.Plugin)), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// stagePlugins creates the plugin directory dir, linking to the plugins of
// the manifest and of the project.
func (d *Deployer) stagePlugins(ctx context.Context, dir string) error {
	if d.Plugins == nil {
		return fmt.Errorf("no store for the plugins")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- mitamae reads the stage as root
		return err
	}
	for _, pl := range d.Project.Plugins {
		hash, err := d.Lock.Hash(pl)
		if err != nil {
			return err
		}
		src, err := d.Plugins.Fetch(ctx, pl, hash)
		if err != nil {
			return err
		}
		if err := os.Symlink(src, filepath.Join(dir, pl.Name())); err != nil {
			return err
		}
	}
	local, err := d.Project.LocalPlugins()
	if err != nil {
		return err
	}
	for _, name := range local {
		link := filepath.Join(dir, name)
		if _, err := os.Lstat(link); err == nil {
			return fmt.Errorf("plugin %s is both in %s and in %s", name, project.FileName, project.PluginsDir)
		}
		if err := os.Symlink(filepath.Join(d.Project.Root, project.PluginsDir, name), link); err != nil {
			return err
		}
	}
	return nil
}

// pluginRecipeExists reports whether the plugin recipe name is found in the
// plugin directory dir.
func pluginRecipeExists(dir, name string) bool {
	plugin := project.PluginOf(name)
	for _, prefix := range []string{"itamae-plugin-recipe-", "mitamae-plugin-recipe-"} {
		for _, file := range project.PluginRecipeFiles(name) {
			info, err := os.Stat(filepath.Join(dir, prefix+plugin, filepath.FromSlash(file)))
			if err == nil && info.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}
