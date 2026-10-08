package deploy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/takumin/manaita/internal/deploy"
	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/plugin"
	"github.com/takumin/manaita/internal/project"
	"github.com/takumin/manaita/internal/testutil"
)

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"plain":               "plain",
		"--node-yaml=a/b.yml": "--node-yaml=a/b.yml",
		"":                    "''",
		"with space":          "'with space'",
		"it's":                `'it'\''s'`,
		"$(rm -rf /)":         "'$(rm -rf /)'",
	}
	for in, want := range cases {
		if got := deploy.Quote(in); got != want {
			t.Errorf("%q: want %s, got %s", in, want, got)
		}
	}
}

func openProject(t *testing.T) *project.Project {
	t.Helper()
	p, err := project.Open(testutil.Project(t))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewPlan(t *testing.T) {
	p := openProject(t)
	h, err := p.HostByFQDN("rpi4.example.internal")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := deploy.NewPlan(p, h, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"helpers/keeper.rb", "cookbooks/common/sudo/default.rb", "cookbooks/server/dnsmasq/extra.rb"}
	if !reflect.DeepEqual(recipePaths(plan), want) {
		t.Errorf("want %v, got %v", want, recipePaths(plan))
	}
	if len(plan.Nodes) != 7 {
		t.Errorf("unexpected nodes: %+v", plan.Nodes)
	}

	plan, err = deploy.NewPlan(p, h, []string{"cookbooks/common/sudo"})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"helpers/keeper.rb", "cookbooks/common/sudo/default.rb"}
	if !reflect.DeepEqual(recipePaths(plan), want) {
		t.Errorf("want %v, got %v", want, recipePaths(plan))
	}
	if !reflect.DeepEqual(p.Prelude, []string{"helpers/keeper.rb"}) {
		t.Errorf("the prelude must not be modified: %v", p.Prelude)
	}

	if _, err := deploy.NewPlan(p, h, []string{"missing"}); err == nil {
		t.Error("expected a missing recipe error")
	}
	empty, err := p.HostByFQDN("empty")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deploy.NewPlan(p, empty, nil); err == nil {
		t.Error("expected an empty run list error")
	}
	p.Nodes = []string{"{unknown}"}
	if _, err := deploy.NewPlan(p, h, nil); err == nil {
		t.Error("expected a node pattern error")
	}
}

func recipePaths(plan *deploy.Plan) []string {
	paths := make([]string, len(plan.Recipes))
	for i, r := range plan.Recipes {
		paths[i] = r.String()
	}
	return paths
}

type fakeRunner struct {
	mu    sync.Mutex
	cmds  [][]string
	stdin []bool
	dirs  []string
	fail  string
	// id is the output of the identification, dsk on aarch64 by default.
	id string
}

func (r *fakeRunner) Run(_ context.Context, cmd *exec.Cmd) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, cmd.Args)
	r.stdin = append(r.stdin, cmd.Stdin != nil)
	r.dirs = append(r.dirs, cmd.Dir)
	if r.fail != "" && strings.Contains(strings.Join(cmd.Args, " "), r.fail) {
		return errors.New("failed")
	}
	if strings.HasPrefix(cmd.Args[len(cmd.Args)-1], "uname -m") {
		id := r.id
		if id == "" {
			id = "aarch64\ndsk\n"
		}
		cmd.Stdout.Write([]byte(id)) //nolint:errcheck,gosec
	}
	return nil
}

func newDeployer(t *testing.T, p *project.Project, runner deploy.Runner) *deploy.Deployer {
	t.Helper()
	body := []byte("mitamae")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body) //nolint:errcheck,gosec
	}))
	t.Cleanup(srv.Close)
	checksum := hex.EncodeToString(sum[:])
	return &deploy.Deployer{
		Project: p,
		Fetcher: &mitamae.Fetcher{
			Version:    "2.0.3",
			Checksums:  map[string]string{"x86_64": checksum, "aarch64": checksum, "armhf": checksum, "i386": checksum},
			CacheDir:   t.TempDir(),
			BaseURL:    srv.URL,
			Downloader: direct(t),
		},
		Options: mitamae.Options{DryRun: true},
		Runner:  runner,
		Stdin:   strings.NewReader(""),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
}

func direct(t *testing.T) *fetch.Fetcher {
	t.Helper()
	dl, err := fetch.New(fetch.Direct)
	if err != nil {
		t.Fatal(err)
	}
	return dl
}

const identify = "uname -m && hostname -s && { dnsdomainname 2>/dev/null || true; }"

func TestRemote(t *testing.T) {
	p := openProject(t)
	runner := &fakeRunner{id: "aarch64\nrpi4\nexample.internal\n"}
	d := newDeployer(t, p, runner)
	d.TTY = true
	if err := d.Remote(context.Background(), "rpi.example"); err != nil {
		t.Fatal(err)
	}

	bin := d.Fetcher.CacheDir + "/v2.0.3/mitamae-aarch64-linux"
	want := [][]string{
		{"ssh", "rpi.example", identify + " && mkdir -p mitamae/.manaita"},
		{"rsync", "-a", "--delete", "--exclude=/.manaita/", "--exclude=/.git/", p.Root + "/", "rpi.example:mitamae/"},
		{"rsync", "-a", bin, "rpi.example:mitamae/.manaita/mitamae"},
		{"ssh", "-t", "rpi.example", "cd mitamae && sudo ./.manaita/mitamae local --dry-run" +
			" --node-json=nodes/all/c.json --node-yaml=nodes/all/a.yml --node-yaml=nodes/all/b.yml" +
			" --node-yaml=nodes/domains/example.internal/d.yml --node-yaml=nodes/domains/example.internal/inventory/x.yml" +
			" --node-yaml=nodes/hosts/rpi4/h.yml --node-yaml=nodes/fqdns/example.internal/rpi4/f.yml" +
			" helpers/keeper.rb cookbooks/common/sudo/default.rb cookbooks/server/dnsmasq/extra.rb"},
	}
	if !reflect.DeepEqual(runner.cmds, want) {
		t.Errorf("want\n%q\ngot\n%q", want, runner.cmds)
	}
	if !reflect.DeepEqual(runner.stdin, []bool{false, false, false, true}) {
		t.Errorf("only the tty session reads stdin: %v", runner.stdin)
	}
}

func TestRemoteDomains(t *testing.T) {
	p := openProject(t)
	cases := map[string]string{
		"aarch64\nrpi4\nexample.internal\n": "cookbooks/common/sudo/default.rb cookbooks/server/dnsmasq/extra.rb",
		"aarch64\nrpi4\nother.internal\n":   "cookbooks/common/sudo/default.rb cookbooks/server/dnsmasq/default.rb",
		"aarch64\nrpi4\n":                   "cookbooks/common/sudo/default.rb",
		"aarch64\nrpi4\nunknown.internal\n": "cookbooks/common/sudo/default.rb",
	}
	for id, want := range cases {
		runner := &fakeRunner{id: id}
		if err := newDeployer(t, p, runner).Remote(context.Background(), "rpi"); err != nil {
			t.Fatal(err)
		}
		last := runner.cmds[len(runner.cmds)-1]
		if !strings.HasSuffix(last[len(last)-1], " helpers/keeper.rb "+want) {
			t.Errorf("%q: unexpected command %q", id, last)
		}
	}
}

func TestRemoteNoTTY(t *testing.T) {
	p := openProject(t)
	runner := &fakeRunner{}
	if err := newDeployer(t, p, runner).Remote(context.Background(), "dsk"); err != nil {
		t.Fatal(err)
	}
	last := runner.cmds[len(runner.cmds)-1]
	if last[1] != "dsk" || runner.stdin[len(runner.stdin)-1] {
		t.Errorf("unexpected session: %q", last)
	}
}

func TestRemoteRecipes(t *testing.T) {
	p := openProject(t)
	runner := &fakeRunner{}
	d := newDeployer(t, p, runner)
	d.Recipes = []string{"cookbooks/server/dnsmasq"}
	if err := d.Remote(context.Background(), "dsk"); err != nil {
		t.Fatal(err)
	}
	last := runner.cmds[len(runner.cmds)-1]
	if !strings.HasSuffix(last[len(last)-1], " helpers/keeper.rb cookbooks/server/dnsmasq/default.rb") {
		t.Errorf("unexpected command: %q", last)
	}
}

func TestRemoteErrors(t *testing.T) {
	p := openProject(t)
	for _, fail := range []string{"uname", "--delete", "/.manaita/mitamae", "sudo"} {
		t.Run(fail, func(t *testing.T) {
			if err := newDeployer(t, p, &fakeRunner{fail: fail}).Remote(context.Background(), "dsk"); err == nil {
				t.Error("expected an error")
			}
		})
	}
	ids := map[string]string{
		"no hostname":    "aarch64\n",
		"unknown host":   "aarch64\nmissing\n",
		"empty run list": "aarch64\nempty\n",
		"unknown arch":   "riscv64\ndsk\n",
	}
	for name, id := range ids {
		t.Run(name, func(t *testing.T) {
			if err := newDeployer(t, p, &fakeRunner{id: id}).Remote(context.Background(), "dsk"); err == nil {
				t.Error("expected an error")
			}
		})
	}

	d := newDeployer(t, p, &fakeRunner{})
	delete(d.Fetcher.Checksums, "aarch64")
	if err := d.Remote(context.Background(), "dsk"); err == nil {
		t.Error("expected a missing checksum error")
	}
}

func TestLocal(t *testing.T) {
	p := openProject(t)
	runner := &fakeRunner{}
	if err := newDeployer(t, p, runner).Local(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.cmds) != 2 || !reflect.DeepEqual(runner.cmds[0], []string{"sh", "-c", identify}) || runner.dirs[1] != p.Root {
		t.Fatalf("unexpected commands: %q in %v", runner.cmds, runner.dirs)
	}
	args := strings.Join(runner.cmds[1], " ")
	if !strings.Contains(args, "/mitamae-") || !strings.HasSuffix(args, "local --dry-run --node-json=nodes/all/c.json --node-yaml=nodes/all/a.yml --node-yaml=nodes/all/b.yml --node-yaml=nodes/hosts/dsk/h.yml helpers/keeper.rb cookbooks/common/sudo/default.rb") {
		t.Errorf("unexpected command: %s", args)
	}
}

func TestLocalPlan(t *testing.T) {
	p := openProject(t)
	runner := &fakeRunner{id: "x86_64\nrpi4\nexample.internal\n"}
	d := newDeployer(t, p, runner)
	arch, plan, err := d.LocalPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := mitamae.LocalArch(); arch != want {
		t.Errorf("want arch %s, got %s", want, arch)
	}
	if plan.Host.FQDN() != "rpi4.example.internal" {
		t.Errorf("unexpected host: %s", plan.Host.FQDN())
	}
	if len(runner.cmds) != 1 {
		t.Errorf("only the identification runs: %q", runner.cmds)
	}
}

func TestLocalCommand(t *testing.T) {
	p := openProject(t)
	d := newDeployer(t, p, &fakeRunner{})
	_, plan, err := d.LocalPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/bin/mitamae", "local", "--dry-run", "--node-json=nodes/all/c.json", "--node-yaml=nodes/all/a.yml", "--node-yaml=nodes/all/b.yml", "--node-yaml=nodes/hosts/dsk/h.yml", "helpers/keeper.rb", "cookbooks/common/sudo/default.rb"}
	for _, name := range []string{"http_proxy", "https_proxy", "ftp_proxy", "all_proxy", "no_proxy", "HTTP_PROXY", "HTTPS_PROXY", "FTP_PROXY", "ALL_PROXY", "NO_PROXY"} {
		t.Setenv(name, "")
	}
	if os.Geteuid() != 0 {
		want = append([]string{"sudo"}, want...)
	}
	if got := d.LocalCommand("/bin/mitamae", plan); !reflect.DeepEqual(got, want) {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestShellCommand(t *testing.T) {
	got := deploy.ShellCommand("/my project", []string{"sudo", "a b", "c"})
	if want := "cd '/my project' && sudo 'a b' c"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestLocalErrors(t *testing.T) {
	p := openProject(t)
	if err := newDeployer(t, p, &fakeRunner{fail: "uname"}).Local(context.Background()); err == nil {
		t.Error("expected an identification error")
	}
	if err := newDeployer(t, p, &fakeRunner{id: "x86_64\nmissing\n"}).Local(context.Background()); err == nil {
		t.Error("expected an unknown host error")
	}
}

func TestPrefixWriter(t *testing.T) {
	var out bytes.Buffer
	var mu sync.Mutex
	a := deploy.NewPrefixWriter(&out, &mu, "[a] ")
	b := deploy.NewPrefixWriter(&out, &mu, "[b] ")

	a.Write([]byte("one\ntw"))  //nolint:errcheck,gosec
	b.Write([]byte("x\n"))      //nolint:errcheck,gosec
	a.Write([]byte("o\nthree")) //nolint:errcheck,gosec
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "[a] one\n[b] x\n[a] two\n[a] three\n"
	if out.String() != want {
		t.Errorf("want %q, got %q", want, out.String())
	}
}

const rev = "0123456789abcdef0123456789abcdef01234567"

// withPlugins declares the apt plugin in the project of d, served with a
// plugin recipe apt::source, locked, and adds a resource plugin to the
// plugin directory of the project.
func withPlugins(t *testing.T, d *deploy.Deployer) {
	t.Helper()
	p := d.Project
	p.Plugins = []project.Plugin{{Repo: "github.com/owner/itamae-plugin-recipe-apt", Rev: rev}}
	testutil.WriteFiles(t, p.Root, map[string]string{
		"plugins/itamae-plugin-resource-local/mrblib/local.rb": "",
	})
	archive := testutil.Archive(t, "top", map[string]string{
		"mrblib/itamae/plugin/recipe/apt/default.rb": "",
		"mrblib/itamae/plugin/recipe/apt/source.rb":  "",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive) //nolint:errcheck,gosec
	}))
	t.Cleanup(srv.Close)
	d.Plugins = plugin.NewStore(t.TempDir(), direct(t))
	d.Plugins.BaseURL = srv.URL
	hash, err := d.Plugins.Download(context.Background(), p.Plugins[0])
	if err != nil {
		t.Fatal(err)
	}
	d.Lock = plugin.Lock{p.Plugins[0]: hash}
	d.StageDir = filepath.Join(t.TempDir(), "stage")
	d.Recipes = []string{"cookbooks/common/sudo", "apt::source", "apt"}
}

func TestRemotePlugins(t *testing.T) {
	p := openProject(t)
	runner := &stagingRunner{}
	d := newDeployer(t, p, runner)
	withPlugins(t, d)
	if err := d.Remote(context.Background(), "dsk"); err != nil {
		t.Fatal(err)
	}

	if len(runner.cmds) != 5 {
		t.Fatalf("unexpected commands: %q", runner.cmds)
	}
	rsync := runner.cmds[3]
	if !reflect.DeepEqual(rsync[:4], []string{"rsync", "-aL", "--delete", "--exclude=/mitamae"}) || rsync[5] != "dsk:mitamae/.manaita/" {
		t.Errorf("unexpected copy of the stage: %q", rsync)
	}
	if _, err := os.Stat(strings.TrimSuffix(rsync[4], "/")); !os.IsNotExist(err) {
		t.Errorf("the stage must be removed: %v", err)
	}
	want := "cd mitamae && sudo ./.manaita/mitamae local --dry-run --plugins=.manaita/plugins" +
		" --node-json=nodes/all/c.json --node-yaml=nodes/all/a.yml --node-yaml=nodes/all/b.yml --node-yaml=nodes/hosts/dsk/h.yml" +
		" helpers/keeper.rb cookbooks/common/sudo/default.rb .manaita/recipes/include.apt.source.rb .manaita/recipes/include.apt.rb"
	if last := runner.cmds[4]; last[len(last)-1] != want {
		t.Errorf("want\n%s\ngot\n%s", want, last[len(last)-1])
	}

	stage := runner.stage
	for file, content := range map[string]string{
		"recipes/include.apt.source.rb": "include_recipe 'apt::source'\n",
		"recipes/include.apt.rb":        "include_recipe 'apt'\n",
	} {
		if got := stage[file]; got != content {
			t.Errorf("%s: want %q, got %q", file, content, got)
		}
	}
	for _, link := range []string{"plugins/itamae-plugin-recipe-apt", "plugins/itamae-plugin-resource-local"} {
		if _, ok := stage[link]; !ok {
			t.Errorf("missing %s in the stage: %v", link, stage)
		}
	}
}

// stagingRunner is a fakeRunner recording the stage when it is copied: the
// content of the files and the targets of the symlinks.
type stagingRunner struct {
	fakeRunner
	stage map[string]string
}

func (r *stagingRunner) Run(ctx context.Context, cmd *exec.Cmd) error {
	if len(cmd.Args) > 1 && cmd.Args[1] == "-aL" {
		r.stage = map[string]string{}
		dir := cmd.Args[len(cmd.Args)-2]
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			if d.Type()&fs.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				r.stage[filepath.ToSlash(rel)] = target
				return err
			}
			data, err := os.ReadFile(path) // #nosec G304 -- a test stage
			r.stage[filepath.ToSlash(rel)] = string(data)
			return err
		})
		if err != nil {
			return err
		}
	}
	return r.fakeRunner.Run(ctx, cmd)
}

func TestLocalPlugins(t *testing.T) {
	p := openProject(t)
	runner := &fakeRunner{}
	d := newDeployer(t, p, runner)
	withPlugins(t, d)
	// A stale stage is replaced.
	testutil.WriteFiles(t, d.StageDir, map[string]string{"recipes/include.stale.rb": ""})
	if err := d.Local(context.Background()); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(runner.cmds[1], " ")
	want := " local --dry-run --plugins=" + d.StageDir + "/plugins" +
		" --node-json=nodes/all/c.json --node-yaml=nodes/all/a.yml --node-yaml=nodes/all/b.yml --node-yaml=nodes/hosts/dsk/h.yml" +
		" helpers/keeper.rb cookbooks/common/sudo/default.rb " + d.StageDir + "/recipes/include.apt.source.rb " + d.StageDir + "/recipes/include.apt.rb"
	if !strings.HasSuffix(args, want) {
		t.Errorf("want suffix\n%s\ngot\n%s", want, args)
	}
	if _, err := os.Stat(filepath.Join(d.StageDir, "recipes/include.stale.rb")); !os.IsNotExist(err) {
		t.Errorf("the stale stage must be removed: %v", err)
	}
	target, err := os.Readlink(filepath.Join(d.StageDir, "plugins", "itamae-plugin-recipe-apt"))
	if err != nil || target != d.Plugins.Path(p.Plugins[0]) {
		t.Errorf("unexpected link to the plugin: %s (%v)", target, err)
	}
	if _, err := os.Stat(filepath.Join(d.StageDir, "plugins", "itamae-plugin-resource-local", "mrblib", "local.rb")); err != nil {
		t.Errorf("the plugins of the project must be linked: %v", err)
	}

	d.StageDir = ""
	if err := d.Local(context.Background()); err == nil {
		t.Error("expected a missing stage directory error")
	}
}

func TestLocalPluginsOfProject(t *testing.T) {
	p := openProject(t)
	testutil.WriteFiles(t, p.Root, map[string]string{
		"plugins/mitamae-plugin-recipe-local/mrblib/mitamae/plugin/recipe/local.rb": "",
	})
	runner := &fakeRunner{}
	d := newDeployer(t, p, runner)
	d.StageDir = filepath.Join(t.TempDir(), "stage")
	d.Recipes = []string{"local"}
	if err := d.Local(context.Background()); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(runner.cmds[1], " ")
	if strings.Contains(args, "--plugins") || !strings.HasSuffix(args, " helpers/keeper.rb "+d.StageDir+"/recipes/include.local.rb") {
		t.Errorf("the plugins of the project are read from ./plugins: %s", args)
	}
	if _, err := os.Stat(filepath.Join(d.StageDir, "plugins")); !os.IsNotExist(err) {
		t.Errorf("no plugin directory is staged: %v", err)
	}
}

func TestPluginErrors(t *testing.T) {
	cases := map[string]func(t *testing.T, d *deploy.Deployer){
		"not locked": func(_ *testing.T, d *deploy.Deployer) {
			d.Lock = plugin.Lock{}
		},
		"hash mismatch": func(_ *testing.T, d *deploy.Deployer) {
			d.Lock[d.Project.Plugins[0]] = "h1:other"
		},
		"no store": func(_ *testing.T, d *deploy.Deployer) {
			d.Plugins = nil
		},
		"missing plugin recipe": func(_ *testing.T, d *deploy.Deployer) {
			d.Recipes = []string{"apt::missing"}
		},
		"same name": func(t *testing.T, d *deploy.Deployer) {
			testutil.WriteFiles(t, d.Project.Root, map[string]string{"plugins/itamae-plugin-recipe-apt/README.md": ""})
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			p := openProject(t)
			for _, local := range []bool{true, false} {
				d := newDeployer(t, p, &fakeRunner{})
				withPlugins(t, d)
				setup(t, d)
				var err error
				if local {
					err = d.Local(context.Background())
				} else {
					err = d.Remote(context.Background(), "dsk")
				}
				if err == nil {
					t.Errorf("local %v: expected an error", local)
				}
				if runner := d.Runner.(*fakeRunner); !local && len(runner.cmds) != 1 {
					t.Errorf("nothing is copied before the plugins are staged: %q", runner.cmds)
				}
			}
		})
	}

	p := openProject(t)
	d := newDeployer(t, p, &fakeRunner{fail: "-aL"})
	withPlugins(t, d)
	if err := d.Remote(context.Background(), "dsk"); err == nil || !strings.Contains(err.Error(), "plugins") {
		t.Errorf("expected a copy error, got %v", err)
	}
}

func TestLocalStageDir(t *testing.T) {
	a := deploy.LocalStageDir("/cache", "/srv/a")
	if filepath.Dir(a) != "/cache/stage" || a == deploy.LocalStageDir("/cache", "/srv/b") || a != deploy.LocalStageDir("/cache", "/srv/a") {
		t.Errorf("unexpected stage directory: %s", a)
	}
}
