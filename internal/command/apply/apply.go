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
	"golang.org/x/term"

	"github.com/takumin/manaita/internal/command/complete"
	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/deploy"
	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/logging"
	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/plugin"
	"github.com/takumin/manaita/internal/project"
)

func NewCommands(cfg *config.Config, flags []cli.Flag) *cli.Command {
	return &cli.Command{
		Name:          "apply",
		Usage:         "apply the run list of hosts, or of this machine without a host",
		ArgsUsage:     "[HOST...]",
		Flags:         append(flags, Flags(cfg)...),
		Action:        Action(cfg, false),
		ShellComplete: complete.Hosts(cfg, true),
	}
}

// Flags returns the flags of apply, shared with plan.
func Flags(cfg *config.Config) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:        "mitamae-log-level",
			Aliases:     []string{"L"},
			Usage:       "log level of mitamae (debug, info, warn, error, fatal)",
			Sources:     cfg.File.Sources("mitamae_log_level"),
			Destination: &cfg.MitamaeLogLevel,
		},
		&cli.StringSliceFlag{
			Name:        "recipe",
			Aliases:     []string{"r"},
			Usage:       "recipe applied instead of the run list (repeatable)",
			Destination: &cfg.Recipes,
		},
		&cli.IntFlag{
			Name:        "parallel",
			Aliases:     []string{"j"},
			Usage:       "number of hosts applied at the same time",
			Sources:     cfg.File.Sources("parallel"),
			Value:       cfg.Parallel,
			Destination: &cfg.Parallel,
		},
	}
}

// Action returns the action applying the run list to the hosts given, or to
// this machine without a host. With dryRun, mitamae only shows the changes.
func Action(cfg *config.Config, dryRun bool) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		dests := cmd.Args().Slice()
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

		dl, err := fetch.New(cfg.Proxy)
		if err != nil {
			return err
		}
		lock, err := plugin.ReadLock(p.Root)
		if err != nil {
			return err
		}
		d := &deploy.Deployer{
			Project:  p,
			Fetcher:  mitamae.NewFetcher(p.Mitamae, cfg.CacheDir, dl),
			Plugins:  plugin.NewStore(mitamae.CacheDir(cfg.CacheDir), dl),
			Lock:     lock,
			StageDir: deploy.LocalStageDir(cfg.CacheDir, p.Root),
			Options: mitamae.Options{
				DryRun:   dryRun,
				LogLevel: cfg.MitamaeLogLevel,
			},
			Recipes: cfg.Recipes,
			Stdin:   cmd.Reader,
			Stdout:  cmd.Writer,
			Stderr:  cmd.ErrWriter,
		}

		switch {
		case len(dests) == 0:
			return d.Local(ctx)
		case len(dests) == 1:
			d.TTY = isTerminal(cmd.Reader)
			return d.Remote(ctx, dests[0])
		default:
			return parallel(ctx, d, dests, cfg.Parallel, cmd.Name)
		}
	}
}

// parallel applies to the ssh destinations dests concurrently, prefixing the
// output with the destination. A failing host does not stop the others. name
// is the command, reported with the failures.
func parallel(ctx context.Context, d *deploy.Deployer, dests []string, limit int, name string) error {
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
				logging.FromContext(ctx).ErrorContext(ctx, "failed to "+name, slog.String("host", dest), slog.Any("error", err))
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
	return ok && term.IsTerminal(int(f.Fd())) // #nosec G115 -- a file descriptor fits in an int
}
