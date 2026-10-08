// Package lock implements the lock command, writing the hashes of the
// plugins to the lock file.
package lock

import (
	"context"
	"log/slog"

	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/config"
	"github.com/takumin/manaita/internal/fetch"
	"github.com/takumin/manaita/internal/logging"
	"github.com/takumin/manaita/internal/mitamae"
	"github.com/takumin/manaita/internal/plugin"
	"github.com/takumin/manaita/internal/project"
)

// NewCommands returns the lock command.
func NewCommands(cfg *config.Config, flags []cli.Flag) *cli.Command {
	return &cli.Command{
		Name:   "lock",
		Usage:  "download the plugins from their origin and write their hashes to " + plugin.LockFileName,
		Flags:  flags,
		Action: action(cfg),
	}
}

func action(cfg *config.Config) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		p, err := project.Open(cfg.Chdir)
		if err != nil {
			return err
		}
		// The hashes are what the downloads through the cache servers are
		// verified against, so they come from the origin only.
		if cfg.Proxy != fetch.Direct {
			logging.FromContext(ctx).DebugContext(ctx, "the proxy list is not used to lock the plugins", slog.String("proxy", cfg.Proxy))
		}
		dl, err := fetch.New(fetch.Direct)
		if err != nil {
			return err
		}
		return plugin.Update(ctx, p, plugin.NewStore(mitamae.CacheDir(cfg.CacheDir), dl))
	}
}
