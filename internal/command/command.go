package command

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/command/dummy"
	"github.com/takumin/manaita/internal/config"
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
		config.LogFormat("json"),
	)

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "log-level",
			Aliases:     []string{"l"},
			Usage:       "log level",
			Sources:     cli.EnvVars("LOG_LEVEL"),
			Value:       cfg.LogLevel,
			Destination: &cfg.LogLevel,
			Action: func(ctx context.Context, cmd *cli.Command, s string) error {
				switch cfg.LogLevel {
				case "debug":
					slog.SetLogLoggerLevel(slog.LevelDebug)
				case "info":
					slog.SetLogLoggerLevel(slog.LevelInfo)
				case "warn":
					slog.SetLogLoggerLevel(slog.LevelWarn)
				case "error":
					slog.SetLogLoggerLevel(slog.LevelError)
				default:
					return fmt.Errorf("unknown log level: %s", cfg.LogLevel)
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name:        "log-format",
			Aliases:     []string{"f"},
			Usage:       "log format",
			Sources:     cli.EnvVars("LOG_FORMAT"),
			Value:       cfg.LogFormat,
			Destination: &cfg.LogFormat,
			Action: func(ctx context.Context, cmd *cli.Command, s string) error {
				switch cfg.LogFormat {
				case "text":
					slog.SetDefault(slog.New(slog.NewTextHandler(cmd.Writer, nil)))
				case "json":
					slog.SetDefault(slog.New(slog.NewJSONHandler(cmd.Writer, nil)))
				default:
					return fmt.Errorf("unknown log format: %s", cfg.LogFormat)
				}
				return nil
			},
		},
	}

	cmds := []*cli.Command{
		dummy.NewCommands(cfg, flags),
	}

	// MEMO: Authors field is invalid in urfave/cli/v3 v3.1.0
	app := &cli.Command{
		Name:                  metadata.AppName(),
		Usage:                 metadata.AppDesc(),
		Version:               fmt.Sprintf("%s (%s)", version.Version(), version.Revision()),
		Flags:                 flags,
		Commands:              cmds,
		EnableShellCompletion: true,
		Reader:                stdin,
		Writer:                stdout,
		ErrWriter:             stderr,
		ExitErrHandler:        func(ctx context.Context, cmd *cli.Command, err error) {},
	}

	ctx := context.Background()
	if err := app.Run(ctx, args); err != nil {
		slog.ErrorContext(ctx, "failed application", slog.Any("error", err))
		return ExitNG
	}

	return ExitOK
}
