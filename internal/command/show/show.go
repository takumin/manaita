package show

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/command/complete"
	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/deploy"
	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/project"
)

func NewCommands(cfg *config.Config, flags []cli.Flag) *cli.Command {
	flags = append(flags, []cli.Flag{
		&cli.StringSliceFlag{
			Name:        "recipe",
			Aliases:     []string{"r"},
			Usage:       "recipe applied instead of the run list (repeatable)",
			Destination: &cfg.Recipes,
		},
	}...)

	return &cli.Command{
		Name:          "show",
		Usage:         "show the node files and recipes applied to a host of the inventory, or to this machine without a host",
		ArgsUsage:     "[HOST]",
		Flags:         flags,
		Action:        action(cfg),
		ShellComplete: complete.Hosts(cfg, false),
	}
}

func action(cfg *config.Config) func(ctx context.Context, cmd *cli.Command) error {
	return func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Args().Len() > 1 {
			return fmt.Errorf("expected at most one host, got %d", cmd.Args().Len())
		}
		p, err := project.Open(cfg.Chdir)
		if err != nil {
			return err
		}
		if cmd.Args().Len() == 0 {
			return showLocal(ctx, cmd, cfg, p)
		}
		h, err := p.HostByFQDN(cmd.Args().First())
		if err != nil {
			return err
		}
		plan, err := deploy.NewPlan(p, h, cfg.Recipes)
		if err != nil {
			return err
		}
		d := &deploy.Deployer{Project: p}
		return print(cmd.Writer, p, plan, d.RemoteCommand(plan))
	}
}

// showLocal prints the plan of this machine, with the command run from the
// project root.
func showLocal(ctx context.Context, cmd *cli.Command, cfg *config.Config, p *project.Project) error {
	fetcher := mitamae.NewFetcher(p.Mitamae, cfg.CacheDir, nil)
	d := &deploy.Deployer{
		Project: p,
		Fetcher: fetcher,
		Recipes: cfg.Recipes,
		Stderr:  cmd.ErrWriter,
	}
	arch, plan, err := d.LocalPlan(ctx)
	if err != nil {
		return err
	}
	return print(cmd.Writer, p, plan, deploy.ShellCommand(p.Root, d.LocalCommand(fetcher.Lookup(arch), plan)))
}

func print(w io.Writer, p *project.Project, plan *deploy.Plan, command string) error {
	domain := plan.Host.Domain
	if domain == "" {
		domain = "-"
	}
	lines := []string{
		"hostname: " + plan.Host.Name,
		"domain:   " + domain,
		"mitamae:  " + p.Mitamae.Version,
		"hosts:",
	}
	for _, f := range plan.Host.Files {
		lines = append(lines, "  - "+f)
	}
	lines = append(lines, "nodes:")
	for _, n := range plan.Nodes {
		lines = append(lines, "  - "+n.Path)
	}
	lines = append(lines, "recipes:")
	for _, r := range plan.Recipes {
		lines = append(lines, "  - "+r)
	}
	lines = append(lines, "command:", "  "+command)
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}
