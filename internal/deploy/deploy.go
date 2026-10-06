// Package deploy applies the recipes of a project to hosts, locally or over
// ssh with the project copied by rsync.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/takumin/manaita/internal/logging"
	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/project"
)

// stateDir is the directory, relative to the remote project, holding the
// files manaita copies besides the project itself.
const stateDir = ".manaita"

// identify prints the arch, the short hostname and the domain of a machine,
// one per line. The domain line is missing when the machine has none.
const identify = "uname -m && hostname -s && { dnsdomainname 2>/dev/null || true; }"

// Plan is what is applied to a host.
type Plan struct {
	Host    *project.Host
	Nodes   []project.NodeFile
	Recipes []string
}

// NewPlan returns the plan of h. recipes override the run list of the host
// when not empty. The prelude of the project always runs first.
func NewPlan(p *project.Project, h *project.Host, recipes []string) (*Plan, error) {
	nodes, err := p.NodeFiles(h)
	if err != nil {
		return nil, err
	}
	if len(recipes) == 0 {
		recipes = h.RunList
	}
	if len(recipes) == 0 {
		return nil, fmt.Errorf("host %s has no run list", h.FQDN())
	}
	resolved, err := p.ResolveRecipes(append(append([]string{}, p.Prelude...), recipes...))
	if err != nil {
		return nil, fmt.Errorf("host %s: %w", h.FQDN(), err)
	}
	return &Plan{Host: h, Nodes: nodes, Recipes: resolved}, nil
}

// Runner runs the commands of a deployment.
type Runner interface {
	Run(ctx context.Context, cmd *exec.Cmd) error
}

// ExecRunner runs the commands as child processes.
type ExecRunner struct{}

// Run runs cmd and waits for it.
func (ExecRunner) Run(_ context.Context, cmd *exec.Cmd) error {
	return cmd.Run()
}

// Deployer applies plans.
type Deployer struct {
	Project *project.Project
	Fetcher *mitamae.Fetcher
	Options mitamae.Options
	Runner  Runner
	// Recipes override the run list of the hosts when not empty.
	Recipes []string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// TTY allocates a terminal on the remote host, so that sudo can prompt
	// for a password.
	TTY bool
}

// LocalPlan returns the arch and the plan of this machine, found in the
// inventory by its hostname and domain.
func (d *Deployer) LocalPlan(ctx context.Context) (string, *Plan, error) {
	var out bytes.Buffer
	if err := d.run(ctx, d.command(ctx, nil, &out, "sh", "-c", identify)); err != nil {
		return "", nil, fmt.Errorf("failed to identify this machine: %w", err)
	}
	_, plan, err := d.plan(ctx, out.String())
	if err != nil {
		return "", nil, err
	}
	arch, err := mitamae.LocalArch()
	if err != nil {
		return "", nil, err
	}
	return arch, plan, nil
}

// Local applies the plan of this machine, found in the inventory by its
// hostname and domain.
func (d *Deployer) Local(ctx context.Context) error {
	arch, plan, err := d.LocalPlan(ctx)
	if err != nil {
		return err
	}
	logging.FromContext(ctx).InfoContext(ctx, "applying to this machine", slog.String("host", plan.Host.FQDN()))
	bin, err := d.Fetcher.Fetch(ctx, arch)
	if err != nil {
		return err
	}
	cmd := d.command(ctx, d.Stdin, d.Stdout, d.LocalCommand(bin, plan)...)
	cmd.Dir = d.Project.Root
	return d.run(ctx, cmd)
}

// LocalCommand returns the command line applying plan on this machine with
// the mitamae binary bin, run from the project root. sudo is prepended unless
// running as root.
func (d *Deployer) LocalCommand(bin string, plan *Plan) []string {
	args := append([]string{bin}, mitamae.LocalArgs(plan.Nodes, plan.Recipes, d.Options)...)
	if os.Geteuid() != 0 {
		args = append([]string{"sudo"}, args...)
	}
	return args
}

// Remote copies the project to the ssh destination dest and applies there the
// plan of the host, found in the inventory by its hostname and domain.
func (d *Deployer) Remote(ctx context.Context, dest string) error {
	dir := d.Project.Remote.Path
	state := path.Join(dir, stateDir)

	var out bytes.Buffer
	prepare := fmt.Sprintf("%s && mkdir -p %s", identify, Quote(state))
	if err := d.run(ctx, d.command(ctx, nil, &out, "ssh", dest, prepare)); err != nil {
		return fmt.Errorf("failed to prepare %s: %w", dest, err)
	}
	machine, plan, err := d.plan(ctx, out.String())
	if err != nil {
		return fmt.Errorf("%s: %w", dest, err)
	}
	arch, err := mitamae.Arch(machine)
	if err != nil {
		return fmt.Errorf("%s: %w", dest, err)
	}
	bin, err := d.Fetcher.Fetch(ctx, arch)
	if err != nil {
		return err
	}

	if err := d.run(ctx, d.command(ctx, nil, d.Stdout, d.RsyncArgs(dest)...)); err != nil {
		return fmt.Errorf("failed to copy the project to %s: %w", dest, err)
	}
	if err := d.run(ctx, d.command(ctx, nil, d.Stdout, "rsync", "-a", bin, dest+":"+path.Join(state, "mitamae"))); err != nil {
		return fmt.Errorf("failed to copy mitamae to %s: %w", dest, err)
	}

	ssh := []string{"ssh"}
	var stdin io.Reader
	if d.TTY {
		ssh = append(ssh, "-t")
		stdin = d.Stdin
	}
	ssh = append(ssh, dest, d.RemoteCommand(plan))
	return d.run(ctx, d.command(ctx, stdin, d.Stdout, ssh...))
}

// plan returns the arch and the plan of the machine identified by out, the
// output of identify.
func (d *Deployer) plan(ctx context.Context, out string) (string, *Plan, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return "", nil, fmt.Errorf("unexpected identification: %q", out)
	}
	name, domain := strings.TrimSpace(lines[1]), ""
	if len(lines) > 2 {
		domain = strings.TrimSpace(lines[2])
	}
	logging.FromContext(ctx).InfoContext(ctx, "identified the host", slog.String("hostname", name), slog.String("domain", domain))
	h, err := d.Project.Host(name, domain)
	if err != nil {
		return "", nil, err
	}
	plan, err := NewPlan(d.Project, h, d.Recipes)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(lines[0]), plan, nil
}

// RsyncArgs returns the command line copying the project to dest.
func (d *Deployer) RsyncArgs(dest string) []string {
	args := []string{"rsync", "-a", "--delete", "--exclude=/" + stateDir + "/"}
	for _, e := range d.Project.Remote.Exclude {
		args = append(args, "--exclude="+e)
	}
	return append(args, d.Project.Root+"/", dest+":"+d.Project.Remote.Path+"/")
}

// RemoteCommand returns the shell command applying plan on the remote host.
func (d *Deployer) RemoteCommand(plan *Plan) string {
	args := append([]string{"sudo", "./" + path.Join(stateDir, "mitamae")}, mitamae.LocalArgs(plan.Nodes, plan.Recipes, d.Options)...)
	return ShellCommand(d.Project.Remote.Path, args)
}

// ShellCommand returns the shell command running args in dir.
func ShellCommand(dir string, args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	return fmt.Sprintf("cd %s && %s", Quote(dir), strings.Join(quoted, " "))
}

func (d *Deployer) command(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) // #nosec G204 -- running ssh, rsync and mitamae is the purpose
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = d.Stderr
	return cmd
}

func (d *Deployer) run(ctx context.Context, cmd *exec.Cmd) error {
	runner := d.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return runner.Run(ctx, cmd)
}

// Quote quotes s for a POSIX shell.
func Quote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@%") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
