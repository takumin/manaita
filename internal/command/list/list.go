package list

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/project"
)

func NewCommands(cfg *config.Config, flags []cli.Flag) *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Usage:   "list the hosts of the inventory",
		Flags:   flags,
		Action:  action(cfg),
	}
}

func action(cfg *config.Config) func(ctx context.Context, cmd *cli.Command) error {
	return func(ctx context.Context, cmd *cli.Command) error {
		p, err := project.Open(cfg.Chdir)
		if err != nil {
			return err
		}
		hosts, err := p.Hosts()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.Writer, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSSH\tHOSTNAME\tDOMAIN\tRECIPES") //nolint:errcheck
		for _, h := range hosts {
			domain := h.Domain
			if domain == "" {
				domain = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", h.Name, h.SSH, h.Hostname, domain, len(h.RunList)) //nolint:errcheck
		}
		return w.Flush()
	}
}
