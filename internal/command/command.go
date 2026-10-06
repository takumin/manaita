package command

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/command/apply"
	"github.com/takumin/manaita/internal/command/list"
	"github.com/takumin/manaita/internal/command/plan"
	"github.com/takumin/manaita/internal/command/show"
	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/logging"
	"github.com/takumin/manaita/internal/metadata"
	"github.com/takumin/manaita/internal/version"
)

const (
	ExitOK int = 0
	ExitNG int = 1
)

func Main(stdout io.Writer, stderr io.Writer, stdin io.Reader, args []string) int {
	cfg := config.NewConfig(
		config.LogLevel("info"),
		config.LogFormat("text"),
		config.Chdir("."),
		config.Parallel(4),
	)

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "log-level",
			Aliases:     []string{"l"},
			Usage:       "log level",
			Sources:     cli.EnvVars("LOG_LEVEL"),
			Value:       cfg.LogLevel,
			Destination: &cfg.LogLevel,
		},
		&cli.StringFlag{
			Name:        "log-format",
			Aliases:     []string{"f"},
			Usage:       "log format",
			Sources:     cli.EnvVars("LOG_FORMAT"),
			Value:       cfg.LogFormat,
			Destination: &cfg.LogFormat,
		},
		&cli.StringFlag{
			Name:        "chdir",
			Aliases:     []string{"C"},
			Usage:       "directory inside the project",
			Sources:     cli.EnvVars("MANAITA_CHDIR"),
			Value:       cfg.Chdir,
			Destination: &cfg.Chdir,
		},
	}

	cmds := []*cli.Command{
		list.NewCommands(cfg, flags),
		show.NewCommands(cfg, flags),
		plan.NewCommands(cfg, flags),
		apply.NewCommands(cfg, flags),
	}

	// The logger is built per run and carried in the context instead of
	// replacing slog.Default, so that concurrent runs do not share it.
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	before := func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
		l, err := logging.New(cmd.ErrWriter, cfg.LogLevel, cfg.LogFormat)
		if err != nil {
			return ctx, err
		}
		logger = l
		return logging.NewContext(ctx, logger), nil
	}

	// MEMO: Authors field is invalid in urfave/cli/v3 v3.1.0
	app := &cli.Command{
		Name:                  metadata.AppName(),
		Usage:                 metadata.AppDesc(),
		Version:               fmt.Sprintf("%s (%s)", version.Version(), version.Revision()),
		Flags:                 flags,
		Commands:              cmds,
		EnableShellCompletion: true,
		Before:                before,
		Reader:                stdin,
		Writer:                stdout,
		ErrWriter:             stderr,
		ExitErrHandler:        func(ctx context.Context, cmd *cli.Command, err error) {},
	}

	ctx := context.Background()
	if err := app.Run(ctx, args); err != nil {
		logger.ErrorContext(ctx, "failed application", slog.Any("error", err))
		return ExitNG
	}

	return ExitOK
}
