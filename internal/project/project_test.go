package project_test

import (
	"os"
	"path/filepath"
	"reflect"
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
	if p.HostsDir != "hosts" || p.Remote.Path != "mitamae" {
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
		{Name: "dsk", SSH: "dsk", Hostname: "dsk", RunList: []string{"cookbooks/common/sudo"}},
		{Name: "empty", SSH: "empty", Hostname: "empty"},
		{Name: "rpi", SSH: "rpi.example", Hostname: "rpi4", Domain: "example.internal", RunList: []string{"cookbooks/server/dnsmasq/extra"}},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("want %+v, got %+v", want, hosts)
	}
}

func TestHost(t *testing.T) {
	p, err := project.Open(testutil.Project(t))
	if err != nil {
		t.Fatal(err)
	}
	if h, err := p.Host("rpi"); err != nil || h.SSH != "rpi.example" {
		t.Errorf("rpi: %+v, %v", h, err)
	}
	for _, name := range []string{"", "missing", "../dsk", ".hidden"} {
		if _, err := p.Host(name); err == nil {
			t.Errorf("%q: expected an error", name)
		}
	}
}

func TestHostInvalidFile(t *testing.T) {
	root := testutil.Project(t)
	testutil.WriteFiles(t, root, map[string]string{"hosts/bad.yml": "run_lists: []\n"})
	p, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Host("bad"); err == nil {
		t.Error("expected an unknown field error")
	}
	if _, err := p.Hosts(); err == nil {
		t.Error("expected an unknown field error")
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
		"rpi": {
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
			h, err := p.Host(name)
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
		"unknown placeholder": {[]string{"nodes/{fqdn}/*.yml"}, project.Host{Hostname: "a"}},
		"bad pattern":         {[]string{"nodes/[/*.yml"}, project.Host{Hostname: "a"}},
		"glob in hostname":    {[]string{"nodes/hosts/{hostname}/*.yml"}, project.Host{Hostname: "*"}},
		"unsupported file":    {[]string{"nodes/all/*"}, project.Host{Hostname: "a"}},
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
	got, err := p.NodeFiles(&project.Host{Hostname: "a"})
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
		if err != nil || got != want {
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
