package show

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/command/complete"
	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/deploy"
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
		Usage:         "show the node files and recipes applied to a host",
		ArgsUsage:     "HOST",
		Flags:         flags,
		Action:        action(cfg),
		ShellComplete: complete.Hosts(cfg, false),
	}
}

func action(cfg *config.Config) func(ctx context.Context, cmd *cli.Command) error {
	return func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Args().Len() != 1 {
			return fmt.Errorf("expected exactly one host, got %d", cmd.Args().Len())
		}
		p, err := project.Open(cfg.Chdir)
		if err != nil {
			return err
		}
		h, err := p.Host(cmd.Args().First())
		if err != nil {
			return err
		}
		plan, err := deploy.NewPlan(p, h, cfg.Recipes)
		if err != nil {
			return err
		}
		return print(cmd.Writer, p, plan)
	}
}

func print(w io.Writer, p *project.Project, plan *deploy.Plan) error {
	domain := plan.Host.Domain
	if domain == "" {
		domain = "-"
	}
	d := &deploy.Deployer{Project: p}
	lines := []string{
		"name:     " + plan.Host.Name,
		"ssh:      " + plan.Host.SSH,
		"hostname: " + plan.Host.Hostname,
		"domain:   " + domain,
		"mitamae:  " + p.Mitamae.Version,
		"nodes:",
	}
	for _, n := range plan.Nodes {
		lines = append(lines, "  - "+n.Path)
	}
	lines = append(lines, "recipes:")
	for _, r := range plan.Recipes {
		lines = append(lines, "  - "+r)
	}
	lines = append(lines, "command:", "  "+d.RemoteCommand(plan))
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}
