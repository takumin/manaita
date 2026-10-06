package plan

import (
	"github.com/urfave/cli/v3"

	"github.com/takumin/manaita/internal/command/apply"
	"github.com/takumin/manaita/internal/command/complete"
	"github.com/takumin/manaita/internal/config"
)

// NewCommands returns the plan command: apply with mitamae in dry-run mode,
// showing the changes without making them.
func NewCommands(cfg *config.Config, flags []cli.Flag) *cli.Command {
	return &cli.Command{
		Name:          "plan",
		Usage:         "show the changes apply would make to hosts, or to this machine without a host",
		ArgsUsage:     "[HOST...]",
		Flags:         append(flags, apply.Flags(cfg)...),
		Action:        apply.Action(cfg, true),
		ShellComplete: complete.Hosts(cfg, true),
	}
}
