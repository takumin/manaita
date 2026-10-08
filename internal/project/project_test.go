package project_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/takumin/manaita/internal/project"
	"github.com/takumin/manaita/internal/testutil"
)

func TestOpen(t *testing.T) {
	root := testutil.Project(t)

	p, err := project.Open(filepath.Join(root, "cookbooks", "common"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != root {
		t.Errorf("root: want %s, got %s", root, p.Root)
	}
	if !slices.Equal(p.Inventory, project.DefaultHosts) || p.Remote.Path != "mitamae" {
		t.Errorf("defaults not applied: %+v", p)
	}
	if p.Mitamae.Version != "2.0.3" || p.Mitamae.Checksums["x86_64"] != "0000" {
		t.Errorf("mitamae: %+v", p.Mitamae)
	}
}

func TestOpenNotFound(t *testing.T) {
	if _, err := project.Open(t.TempDir()); err == nil {
		t.Error("expected an error")
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"no version":    "nodes: []\n",
		"unknown field": "mitamae:\n  version: 1\nunknown: 1\n",
		"absolute path": "mitamae:\n  version: 1\nremote:\n  path: /srv\n",
		"invalid yaml":  "mitamae: [\n",
		"hosts string":  "mitamae:\n  version: 1\nhosts: hosts\n",
		"plugin host":   plugins("gitlab.com/o/itamae-plugin-recipe-a", rev),
		"plugin path":   plugins("github.com/o/x/itamae-plugin-recipe-a", rev),
		"plugin dot":    plugins("github.com/o/..", rev),
		"plugin owner":  plugins("github.com/../itamae-plugin-recipe-a", rev),
		"plugin name":   plugins("github.com/o/apt", rev),
		"plugin kind":   plugins("github.com/o/itamae-plugin-other-a", rev),
		"plugin branch": plugins("github.com/o/itamae-plugin-recipe-a", "main"),
		"plugin short":  plugins("github.com/o/itamae-plugin-recipe-a", rev[:7]),
		"plugin upper":  plugins("github.com/o/itamae-plugin-recipe-a", strings.ToUpper(rev)),
		"plugin twice": plugins("github.com/o/itamae-plugin-recipe-a", rev) +
			"  - repo: github.com/p/itamae-plugin-recipe-a\n    rev: " + rev + "\n",
	}
	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			testutil.WriteFiles(t, root, map[string]string{project.FileName: manifest})
			if _, err := project.Load(root); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

const rev = "0123456789abcdef0123456789abcdef01234567"

// plugins returns a manifest declaring the plugin repo at rev.
func plugins(repo, rev string) string {
	return "mitamae:\n  version: 1\nplugins:\n  - repo: " + repo + "\n    rev: " + rev + "\n"
}

func TestLoadPlugins(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFiles(t, root, map[string]string{
		project.FileName: plugins("github.com/o/itamae-plugin-recipe-a", rev) +
			"  - repo: github.com/o/mitamae-plugin-resource-b.c\n    rev: " + rev + "\n",
	})
	p, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Plugins) != 2 || p.Plugins[0].Name() != "itamae-plugin-recipe-a" || p.Plugins[1].String() != "github.com/o/mitamae-plugin-resource-b.c@"+rev {
		t.Errorf("unexpected plugins: %+v", p.Plugins)
	}
}

func TestLoadEmpty(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFiles(t, root, map[string]string{project.FileName: ""})
	if _, err := project.Load(root); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("expected a missing version error, got %v", err)
	}
}

func TestHosts(t *testing.T) {
	p, err := project.Open(testutil.Project(t))
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := p.Hosts()
	if err != nil {
		t.Fatal(err)
	}
	want := []*project.Host{
		{Name: "dsk", RunList: []string{"cookbooks/common/sudo"}, Files: []string{"hosts/all/common.yml", "hosts/hosts/dsk/run_list.yml"}},
		{Name: "empty", RunList: []string{}, Files: []string{"hosts/all/common.yml", "hosts/hosts/empty/host.yml"}},
		{Name: "rpi4", RunList: []string{"cookbooks/common/sudo"}, Files: []string{"hosts/all/common.yml", "hosts/hosts/rpi4/run_list.yml", "hosts/hosts/rpi4/z.yaml"}},
		{
			Name:    "rpi4",
			Domain:  "example.internal",
			RunList: []string{"cookbooks/common/sudo", "cookbooks/server/dnsmasq/extra"},
			Files: []string{
				"hosts/all/common.yml",
				"hosts/domains/example.internal/common.yml",
				"hosts/hosts/rpi4/run_list.yml",
				"hosts/hosts/rpi4/z.yaml",
				"hosts/fqdns/example.internal/rpi4/run_list.yml",
			},
		},
		{
			Name:    "rpi4",
			Domain:  "other.internal",
			RunList: []string{"cookbooks/common/sudo", "cookbooks/server/dnsmasq"},
			Files: []string{
				"hosts/all/common.yml",
				"hosts/hosts/rpi4/run_list.yml",
				"hosts/hosts/rpi4/z.yaml",
				"hosts/fqdns/other.internal/rpi4/run_list.yml",
			},
		},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("want %+v, got %+v", want, hosts)
	}
}

func TestHostsPatterns(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFiles(t, root, map[string]string{
		project.FileName: "mitamae:\n  version: 1\nhosts:\n" +
			"  - nodes/all/recipes/*.yml\n" +
			"  - nodes/domains/{domain}/recipes/*.yml\n" +
			"  - nodes/hosts/{hostname}/recipes/*.yml\n" +
			"  - inventory/{hostname}.yml\n" +
			"  - inventory/{hostname}.{domain}.yml\n" +
			"  - inventory/{hostname}/{hostname}-[a-z]?.yml\n",
		"nodes/all/recipes/common.yml":                 "run_list:\n  - helpers/keeper.rb\n",
		"nodes/all/config/a.yml":                       "",
		"nodes/domains/example.internal/recipes/d.yml": "run_list:\n  - cookbooks/common/sudo\n",
		"nodes/hosts/dsk/recipes/host.yml":             "",
		"nodes/hosts/dsk/config/h.yml":                 "",
		"nodes/hosts/.hidden/recipes/host.yml":         "",
		"nodes/hosts/noconfig/config/h.yml":            "",
		"nodes/hosts/dir/recipes/dir.yml/x":            "",
		"inventory/gw.yml":                             "",
		"inventory/rpi4.example.internal.yml":          "run_list:\n  - cookbooks/server/dnsmasq\n",
		"inventory/.hidden.example.internal.yml":       "",
		"inventory/nas/nas-a1.yml":                     "",
		"inventory/nas/other-a1.yml":                   "",
	})
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := p.Hosts()
	if err != nil {
		t.Fatal(err)
	}
	want := []*project.Host{
		{Name: "dsk", RunList: []string{"helpers/keeper.rb"}, Files: []string{"nodes/all/recipes/common.yml", "nodes/hosts/dsk/recipes/host.yml"}},
		{Name: "gw", RunList: []string{"helpers/keeper.rb"}, Files: []string{"nodes/all/recipes/common.yml", "inventory/gw.yml"}},
		{Name: "nas", RunList: []string{"helpers/keeper.rb"}, Files: []string{"nodes/all/recipes/common.yml", "inventory/nas/nas-a1.yml"}},
		{
			Name:    "rpi4",
			Domain:  "example.internal",
			RunList: []string{"helpers/keeper.rb", "cookbooks/common/sudo", "cookbooks/server/dnsmasq"},
			Files: []string{
				"nodes/all/recipes/common.yml",
				"nodes/domains/example.internal/recipes/d.yml",
				"inventory/rpi4.example.internal.yml",
			},
		},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("want %+v, got %+v", want, hosts)
	}
	for _, fqdn := range []string{"noconfig", "dir", "other"} {
		if _, err := p.HostByFQDN(fqdn); err == nil {
			t.Errorf("%s: expected the host not to be declared", fqdn)
		}
	}
}

func TestHostsErrors(t *testing.T) {
	cases := map[string][]string{
		"unknown placeholder": {"hosts/{hostname}/{fqdn}/*.yml"},
		"bad pattern":         {"hosts/{hostname}/[/*.yml"},
		"unknown in layer":    {"hosts/{fqdn}/*.yml", "hosts/hosts/{hostname}/*.yml"},
	}
	root := testutil.Project(t)
	for name, inventory := range cases {
		t.Run(name, func(t *testing.T) {
			p := &project.Project{Root: root, Inventory: inventory}
			if _, err := p.Hosts(); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestHost(t *testing.T) {
	root := testutil.Project(t)
	testutil.WriteFiles(t, root, map[string]string{
		"hosts/all/common.yml":                   "run_list:\n  - helpers/keeper.rb\n",
		"hosts/fqdns/other.internal/gw/host.yml": "",
		"hosts/hosts/file":                       "",
	})
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]project.Host{
		"dsk": {
			Name:    "dsk",
			RunList: []string{"helpers/keeper.rb", "cookbooks/common/sudo"},
			Files:   []string{"hosts/all/common.yml", "hosts/hosts/dsk/run_list.yml"},
		},
		// A domain without its own layers still gets the layers of the host.
		"dsk.unknown.internal": {
			Name:    "dsk",
			Domain:  "unknown.internal",
			RunList: []string{"helpers/keeper.rb", "cookbooks/common/sudo"},
			Files:   []string{"hosts/all/common.yml", "hosts/hosts/dsk/run_list.yml"},
		},
		"gw.other.internal": {
			Name:    "gw",
			Domain:  "other.internal",
			RunList: []string{"helpers/keeper.rb"},
			Files:   []string{"hosts/all/common.yml", "hosts/fqdns/other.internal/gw/host.yml"},
		},
	}
	for fqdn, want := range cases {
		h, err := p.HostByFQDN(fqdn)
		if err != nil {
			t.Errorf("%s: %v", fqdn, err)
			continue
		}
		if !reflect.DeepEqual(*h, want) {
			t.Errorf("%s: want %+v, got %+v", fqdn, want, *h)
		}
		if h.FQDN() != fqdn {
			t.Errorf("%s: unexpected fqdn %s", fqdn, h.FQDN())
		}
	}
	for _, fqdn := range []string{"", "missing", "gw", "gw.example.internal", ".hidden", "*", "file", "nofiles", "all", "dsk..x", "dsk.a/b"} {
		if _, err := p.HostByFQDN(fqdn); err == nil {
			t.Errorf("%q: expected an error", fqdn)
		}
	}
	if _, err := p.Host("../dsk", ""); err == nil {
		t.Error("expected an invalid hostname error")
	}
}

func TestHostInvalidFile(t *testing.T) {
	cases := map[string]string{
		"host":   "hosts/hosts/dsk/run_list.yml",
		"all":    "hosts/all/common.yml",
		"domain": "hosts/domains/example.internal/common.yml",
		"fqdn":   "hosts/fqdns/example.internal/rpi4/run_list.yml",
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			root := testutil.Project(t)
			testutil.WriteFiles(t, root, map[string]string{file: "run_lists: []\n"})
			p, err := project.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Hosts(); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestNodeFiles(t *testing.T) {
	p, err := project.Open(testutil.Project(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]project.NodeFile{
		"dsk": {
			{Path: "nodes/all/c.json", Format: project.FormatJSON},
			{Path: "nodes/all/a.yml", Format: project.FormatYAML},
			{Path: "nodes/all/b.yml", Format: project.FormatYAML},
			{Path: "nodes/hosts/dsk/h.yml", Format: project.FormatYAML},
		},
		"rpi4.example.internal": {
			{Path: "nodes/all/c.json", Format: project.FormatJSON},
			{Path: "nodes/all/a.yml", Format: project.FormatYAML},
			{Path: "nodes/all/b.yml", Format: project.FormatYAML},
			{Path: "nodes/domains/example.internal/d.yml", Format: project.FormatYAML},
			{Path: "nodes/domains/example.internal/inventory/x.yml", Format: project.FormatYAML},
			{Path: "nodes/hosts/rpi4/h.yml", Format: project.FormatYAML},
			{Path: "nodes/fqdns/example.internal/rpi4/f.yml", Format: project.FormatYAML},
		},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			h, err := p.HostByFQDN(name)
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.NodeFiles(h)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("want %+v, got %+v", want, got)
			}
		})
	}
}

func TestNodeFilesErrors(t *testing.T) {
	cases := map[string]struct {
		nodes []string
		host  project.Host
	}{
		"unknown placeholder": {[]string{"nodes/{fqdn}/*.yml"}, project.Host{Name: "a"}},
		"bad pattern":         {[]string{"nodes/[/*.yml"}, project.Host{Name: "a"}},
		"glob in hostname":    {[]string{"nodes/hosts/{hostname}/*.yml"}, project.Host{Name: "*"}},
		"unsupported file":    {[]string{"nodes/all/*"}, project.Host{Name: "a"}},
	}
	root := testutil.Project(t)
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			p := &project.Project{Root: root, Nodes: tt.nodes}
			if _, err := p.NodeFiles(&tt.host); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestNodeFilesSkipsDirectories(t *testing.T) {
	root := testutil.Project(t)
	if err := os.Mkdir(filepath.Join(root, "nodes/all/dir.yml"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &project.Project{Root: root, Nodes: []string{"nodes/all/*.yml"}}
	got, err := p.NodeFiles(&project.Host{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 files, got %+v", got)
	}
}

func TestResolveRecipe(t *testing.T) {
	p, err := project.Open(testutil.Project(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"cookbooks/common/sudo":              "cookbooks/common/sudo/default.rb",
		"cookbooks/common/sudo/":             "cookbooks/common/sudo/default.rb",
		"cookbooks/server/dnsmasq/extra":     "cookbooks/server/dnsmasq/extra.rb",
		"cookbooks/server/dnsmasq/extra.rb":  "cookbooks/server/dnsmasq/extra.rb",
		"./helpers/keeper.rb":                "helpers/keeper.rb",
		"cookbooks/../cookbooks/common/sudo": "cookbooks/common/sudo/default.rb",
	}
	for name, want := range cases {
		got, err := p.ResolveRecipe(name)
		if err != nil || got != (project.Recipe{Path: want}) {
			t.Errorf("%s: want %s, got %s (%v)", name, want, got, err)
		}
	}
	for _, name := range []string{"cookbooks/missing", "cookbooks", "/etc/passwd", "../x", ".."} {
		if _, err := p.ResolveRecipe(name); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := p.ResolveRecipes([]string{"helpers/keeper.rb", "missing"}); err == nil {
		t.Error("expected an error")
	}
}

func TestResolvePluginRecipe(t *testing.T) {
	root := testutil.Project(t)
	testutil.WriteFiles(t, root, map[string]string{
		project.FileName: testutil.Manifest + "plugins:\n- repo: github.com/o/itamae-plugin-recipe-apt\n  rev: " + rev + "\n",
		"plugins/mitamae-plugin-recipe-local/README.md":  "",
		"plugins/.itamae-plugin-recipe-hidden/README.md": "",
		"plugins/itamae-plugin-recipe-file":              "",
		"cookbooks/apt.rb":                               "",
	})
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]project.Recipe{
		"apt::source":           {Plugin: "apt::source"},
		"apt::a::b":             {Plugin: "apt::a::b"},
		"local":                 {Plugin: "local"},
		"local::x-y_z":          {Plugin: "local::x-y_z"},
		"cookbooks/apt":         {Path: "cookbooks/apt.rb"},
		"cookbooks/common/sudo": {Path: "cookbooks/common/sudo/default.rb"},
	}
	for name, want := range cases {
		got, err := p.ResolveRecipe(name)
		if err != nil || got != want {
			t.Errorf("%s: want %+v, got %+v (%v)", name, want, got, err)
		}
		if got.String() != name && got.Plugin != "" {
			t.Errorf("%s: unexpected name %s", name, got)
		}
	}
	for _, name := range []string{"missing", "hidden", "file", "apt::", "apt::a.rb", "apt:source", "resource"} {
		if r, err := p.ResolveRecipe(name); err == nil {
			t.Errorf("%s: expected an error, got %+v", name, r)
		}
	}

	local, err := p.LocalPlugins()
	if err != nil || len(local) != 1 || local[0] != "mitamae-plugin-recipe-local" {
		t.Errorf("unexpected local plugins: %v (%v)", local, err)
	}
}

func TestPluginRecipeFiles(t *testing.T) {
	want := []string{
		"mrblib/itamae/plugin/recipe/apt/default.rb",
		"mrblib/itamae/plugin/recipe/apt.rb",
		"mrblib/mitamae/plugin/recipe/apt/default.rb",
		"mrblib/mitamae/plugin/recipe/apt.rb",
	}
	if got := project.PluginRecipeFiles("apt"); !slices.Equal(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
	want = []string{"mrblib/itamae/plugin/recipe/apt/a/b.rb", "mrblib/mitamae/plugin/recipe/apt/a/b.rb"}
	if got := project.PluginRecipeFiles("apt::a::b"); !slices.Equal(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
	if got := project.PluginOf("apt::a::b"); got != "apt" {
		t.Errorf("unexpected plugin: %s", got)
	}
}
