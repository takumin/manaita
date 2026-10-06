// Package complete provides the shell completion of the subcommands.
package complete

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/project"
)

// Hosts returns a completion suggesting the hosts of the inventory. The hosts
// already given are not suggested again, and nothing is suggested once a host
// is given unless multiple is true. A flag being completed falls back to the
// default completion.
func Hosts(cfg *config.Config, multiple bool) cli.ShellCompleteFunc {
	return func(ctx context.Context, cmd *cli.Command) {
		args := cmd.Args().Slice()
		if len(args) > 0 && strings.HasPrefix(args[len(args)-1], "-") {
			cli.DefaultCompleteWithFlags(ctx, cmd)
			return
		}
		if len(args) > 0 && !multiple {
			return
		}
		// Errors are ignored: a completion has nowhere to report them.
		p, err := project.Open(cfg.Chdir)
		if err != nil {
			return
		}
		hosts, err := p.Hosts()
		if err != nil {
			return
		}
		for _, h := range hosts {
			if slices.Contains(args, h.Name) {
				continue
			}
			fmt.Fprintln(cmd.Root().Writer, h.Name) //nolint:errcheck
		}
	}
}
