package deploy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/takumin/manaita/internal/deploy"
	"github.com/takumin/manaita/internal/mitamae"
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
	if !reflect.DeepEqual(plan.Recipes, want) {
		t.Errorf("want %v, got %v", want, plan.Recipes)
	}
	if len(plan.Nodes) != 7 {
		t.Errorf("unexpected nodes: %+v", plan.Nodes)
	}

	plan, err = deploy.NewPlan(p, h, []string{"cookbooks/common/sudo"})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"helpers/keeper.rb", "cookbooks/common/sudo/default.rb"}
	if !reflect.DeepEqual(plan.Recipes, want) {
		t.Errorf("want %v, got %v", want, plan.Recipes)
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
			Version:   "2.0.3",
			Checksums: map[string]string{"x86_64": checksum, "aarch64": checksum, "armhf": checksum, "i386": checksum},
			CacheDir:  t.TempDir(),
			BaseURL:   srv.URL,
			Client:    srv.Client(),
		},
		Options: mitamae.Options{DryRun: true},
		Runner:  runner,
		Stdin:   strings.NewReader(""),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
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
