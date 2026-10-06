package apply

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"

	"github.com/takumin/manaita/internal/command/complete"
	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/deploy"
	"github.com/takumin/manaita/internal/logging"
	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/project"
)

func NewCommands(cfg *config.Config, flags []cli.Flag) *cli.Command {
	flags = append(flags, []cli.Flag{
		&cli.BoolFlag{
			Name:        "dry-run",
			Aliases:     []string{"n"},
			Usage:       "show the changes without applying them",
			Destination: &cfg.DryRun,
		},
		&cli.StringFlag{
			Name:        "mitamae-log-level",
			Aliases:     []string{"L"},
			Usage:       "log level of mitamae (debug, info, warn, error, fatal)",
			Destination: &cfg.MitamaeLogLevel,
		},
		&cli.StringSliceFlag{
			Name:        "recipe",
			Aliases:     []string{"r"},
			Usage:       "recipe applied instead of the run list (repeatable)",
			Destination: &cfg.Recipes,
		},
		&cli.BoolFlag{
			Name:        "local",
			Usage:       "apply to this machine instead of over ssh",
			Destination: &cfg.Local,
		},
		&cli.IntFlag{
			Name:        "parallel",
			Aliases:     []string{"j"},
			Usage:       "number of hosts applied at the same time",
			Value:       cfg.Parallel,
			Destination: &cfg.Parallel,
		},
	}...)

	return &cli.Command{
		Name:          "apply",
		Usage:         "apply the run list of hosts",
		ArgsUsage:     "HOST...",
		Flags:         flags,
		Action:        action(cfg),
		ShellComplete: complete.Hosts(cfg, true),
	}
}

func action(cfg *config.Config) func(ctx context.Context, cmd *cli.Command) error {
	return func(ctx context.Context, cmd *cli.Command) error {
		dests := cmd.Args().Slice()
		switch {
		case cfg.Local && len(dests) > 0:
			return errors.New("--local applies to this machine and takes no host")
		case !cfg.Local && len(dests) == 0:
			return errors.New("no host given")
		}
		if cfg.Parallel < 1 {
			return fmt.Errorf("invalid --parallel: %d", cfg.Parallel)
		}

		p, err := project.Open(cfg.Chdir)
		if err != nil {
			return err
		}
		// The hosts are only known once connected, so the recipes given are
		// checked beforehand.
		if _, err := p.ResolveRecipes(cfg.Recipes); err != nil {
			return err
		}

		fetcher, err := mitamae.NewFetcher(p.Mitamae)
		if err != nil {
			return err
		}
		d := &deploy.Deployer{
			Project: p,
			Fetcher: fetcher,
			Options: mitamae.Options{
				DryRun:   cfg.DryRun,
				LogLevel: cfg.MitamaeLogLevel,
			},
			Recipes: cfg.Recipes,
			Stdin:   cmd.Reader,
			Stdout:  cmd.Writer,
			Stderr:  cmd.ErrWriter,
		}

		switch {
		case cfg.Local:
			return d.Local(ctx)
		case len(dests) == 1:
			d.TTY = isTerminal(cmd.Reader)
			return d.Remote(ctx, dests[0])
		default:
			return parallel(ctx, d, dests, cfg.Parallel)
		}
	}
}

// parallel applies to the ssh destinations dests concurrently, prefixing the
// output with the destination. A failing host does not stop the others.
func parallel(ctx context.Context, d *deploy.Deployer, dests []string, limit int) error {
	var mu sync.Mutex
	g := errgroup.Group{}
	g.SetLimit(limit)
	errs := make([]error, len(dests))
	for i, dest := range dests {
		g.Go(func() error {
			prefix := fmt.Sprintf("[%s] ", dest)
			stdout := deploy.NewPrefixWriter(d.Stdout, &mu, prefix)
			stderr := deploy.NewPrefixWriter(d.Stderr, &mu, prefix)
			hd := *d
			hd.Stdin = nil
			hd.Stdout = stdout
			hd.Stderr = stderr
			err := hd.Remote(ctx, dest)
			_ = stdout.Flush()
			_ = stderr.Flush()
			if err != nil {
				logging.FromContext(ctx).ErrorContext(ctx, "failed to apply", slog.String("host", dest), slog.Any("error", err))
				errs[i] = fmt.Errorf("%s: %w", dest, err)
			}
			return nil
		})
	}
	_ = g.Wait()
	return errors.Join(errs...)
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
