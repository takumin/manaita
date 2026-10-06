// Package deploy applies the recipes of a project to hosts, locally or over
// ssh with the project copied by rsync.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/project"
)

// stateDir is the directory, relative to the remote project, holding the
// files manaita copies besides the project itself.
const stateDir = ".manaita"

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
		return nil, fmt.Errorf("host %s has no run list", h.Name)
	}
	resolved, err := p.ResolveRecipes(append(append([]string{}, p.Prelude...), recipes...))
	if err != nil {
		return nil, fmt.Errorf("host %s: %w", h.Name, err)
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

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// TTY allocates a terminal on the remote host, so that sudo can prompt
	// for a password.
	TTY bool
}

// Local applies plan to this machine.
func (d *Deployer) Local(ctx context.Context, plan *Plan) error {
	arch, err := mitamae.LocalArch()
	if err != nil {
		return err
	}
	bin, err := d.Fetcher.Fetch(ctx, arch)
	if err != nil {
		return err
	}
	args := append([]string{bin}, mitamae.LocalArgs(plan.Nodes, plan.Recipes, d.Options)...)
	if os.Geteuid() != 0 {
		args = append([]string{"sudo"}, args...)
	}
	cmd := d.command(ctx, d.Stdin, d.Stdout, args...)
	cmd.Dir = d.Project.Root
	return d.run(ctx, cmd)
}

// Remote copies the project to the host of plan and applies plan there.
func (d *Deployer) Remote(ctx context.Context, plan *Plan) error {
	dest := plan.Host.SSH
	dir := d.Project.Remote.Path
	state := path.Join(dir, stateDir)

	var out bytes.Buffer
	prepare := fmt.Sprintf("uname -m && mkdir -p %s", Quote(state))
	if err := d.run(ctx, d.command(ctx, nil, &out, "ssh", dest, prepare)); err != nil {
		return fmt.Errorf("failed to prepare %s: %w", dest, err)
	}
	arch, err := mitamae.Arch(firstLine(out.String()))
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
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	return fmt.Sprintf("cd %s && %s", Quote(d.Project.Remote.Path), strings.Join(quoted, " "))
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// Quote quotes s for a POSIX shell.
func Quote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@%") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
