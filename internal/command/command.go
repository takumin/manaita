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
	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/logging"
	"github.com/takumin/manaita/internal/metadata"
	"github.com/takumin/manaita/internal/mitamae"
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
		config.ConfigFile(config.DefaultFile),
	)
	cfg.File = config.NewFile(&cfg.ConfigFile)

	// The configuration file comes first, so that its path is known before
	// the other flags read their values from it.
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "config",
			Usage:       "configuration file of this machine, holding the flags by their names with underscores (empty for none)",
			Sources:     cli.EnvVars("MANAITA_CONFIG"),
			Value:       cfg.ConfigFile,
			Destination: &cfg.ConfigFile,
		},
		&cli.StringFlag{
			Name:        "log-level",
			Aliases:     []string{"l"},
			Usage:       "log level",
			Sources:     cfg.File.Sources("log_level", "LOG_LEVEL"),
			Value:       cfg.LogLevel,
			Destination: &cfg.LogLevel,
		},
		&cli.StringFlag{
			Name:        "log-format",
			Aliases:     []string{"f"},
			Usage:       "log format",
			Sources:     cfg.File.Sources("log_format", "LOG_FORMAT"),
			Value:       cfg.LogFormat,
			Destination: &cfg.LogFormat,
		},
		&cli.StringFlag{
			Name:        "chdir",
			Aliases:     []string{"C"},
			Usage:       "directory inside the project",
			Sources:     cfg.File.Sources("chdir", "MANAITA_CHDIR"),
			Value:       cfg.Chdir,
			Destination: &cfg.Chdir,
		},
		&cli.StringFlag{
			Name:        "cache-dir",
			Usage:       "cache directory (default: manaita under the user cache directory, else " + mitamae.SystemCacheDir + ")",
			Sources:     cfg.File.Sources("cache_dir", "MANAITA_CACHE_DIR"),
			Destination: &cfg.CacheDir,
		},
		&cli.StringFlag{
			Name:        "proxy",
			Usage:       "cache servers to download through, like GOPROXY (e.g. http://cache.internal|direct)",
			Sources:     cfg.File.Sources("proxy", "MANAITA_PROXY"),
			Value:       fetch.Direct,
			Destination: &cfg.Proxy,
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
		if err := cfg.File.Load(); err != nil {
			return ctx, err
		}
		l, err := logging.New(cmd.ErrWriter, cfg.LogLevel, cfg.LogFormat)
		if err != nil {
			return ctx, err
		}
		logger = l
		return logging.NewContext(ctx, logger), nil
	}

	ver := version.Version()
	if pre := version.Prerelease(); pre != "" {
		ver += "-" + pre
	}

	// MEMO: Authors field is invalid in urfave/cli/v3 v3.1.0
	app := &cli.Command{
		Name:                  metadata.AppName(),
		Usage:                 metadata.AppDesc(),
		Version:               fmt.Sprintf("%s (%s)", ver, version.Revision()),
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
