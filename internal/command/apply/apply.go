package apply

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"

	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/deploy"
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
			Usage:       "apply to this machine instead of over ssh (HOST defaults to the short hostname)",
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
		Name:      "apply",
		Usage:     "apply the run list of hosts",
		ArgsUsage: "HOST...",
		Flags:     flags,
		Action:    action(cfg),
	}
}

func action(cfg *config.Config) func(ctx context.Context, cmd *cli.Command) error {
	return func(ctx context.Context, cmd *cli.Command) error {
		names := cmd.Args().Slice()
		if cfg.Local {
			switch len(names) {
			case 0:
				hostname, err := os.Hostname()
				if err != nil {
					return fmt.Errorf("failed to get the hostname: %w", err)
				}
				name, _, _ := strings.Cut(hostname, ".")
				names = []string{name}
			case 1:
			default:
				return errors.New("--local applies to a single host")
			}
		} else if len(names) == 0 {
			return errors.New("no host given")
		}
		if cfg.Parallel < 1 {
			return fmt.Errorf("invalid --parallel: %d", cfg.Parallel)
		}

		p, err := project.Open(cfg.Chdir)
		if err != nil {
			return err
		}
		plans := make([]*deploy.Plan, 0, len(names))
		for _, name := range names {
			h, err := p.Host(name)
			if err != nil {
				return err
			}
			plan, err := deploy.NewPlan(p, h, cfg.Recipes)
			if err != nil {
				return err
			}
			plans = append(plans, plan)
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
			Stdin:  cmd.Reader,
			Stdout: cmd.Writer,
			Stderr: cmd.ErrWriter,
		}

		switch {
		case cfg.Local:
			return d.Local(ctx, plans[0])
		case len(plans) == 1:
			d.TTY = isTerminal(cmd.Reader)
			return d.Remote(ctx, plans[0])
		default:
			return parallel(ctx, d, plans, cfg.Parallel)
		}
	}
}

// parallel applies plans to their hosts concurrently, prefixing the output
// with the host name. A failing host does not stop the others.
func parallel(ctx context.Context, d *deploy.Deployer, plans []*deploy.Plan, limit int) error {
	var mu sync.Mutex
	g := errgroup.Group{}
	g.SetLimit(limit)
	errs := make([]error, len(plans))
	for i, plan := range plans {
		g.Go(func() error {
			prefix := fmt.Sprintf("[%s] ", plan.Host.Name)
			stdout := deploy.NewPrefixWriter(d.Stdout, &mu, prefix)
			stderr := deploy.NewPrefixWriter(d.Stderr, &mu, prefix)
			hd := *d
			hd.Stdin = nil
			hd.Stdout = stdout
			hd.Stderr = stderr
			err := hd.Remote(ctx, plan)
			_ = stdout.Flush()
			_ = stderr.Flush()
			if err != nil {
				slog.ErrorContext(ctx, "failed to apply", slog.String("host", plan.Host.Name), slog.Any("error", err))
				errs[i] = fmt.Errorf("%s: %w", plan.Host.Name, err)
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
