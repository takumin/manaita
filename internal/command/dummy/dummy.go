package dummy

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/config"
)

func NewCommands(cfg *config.Config, flags []cli.Flag) *cli.Command {
	flags = append(flags, []cli.Flag{
		&cli.StringFlag{
			Name:        "variable",
			Aliases:     []string{"v"},
			Usage:       "variable",
			Sources:     cli.EnvVars("VARIABLE"),
			Value:       cfg.Variable,
			Destination: &cfg.Variable,
		},
	}...)
	return &cli.Command{
		Name:    "dummy",
		Aliases: []string{"d"},
		Usage:   "dummy",
		Flags:   flags,
		Action:  action(cfg),
	}
}

func action(cfg *config.Config) func(ctx context.Context, cmd *cli.Command) error {
	return func(ctx context.Context, cmd *cli.Command) error {
		return nil
	}
}
