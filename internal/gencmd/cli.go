package gencmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/urfave/cli/v3"
)

// Command builds the `generations` subcommand (list, clean, rollback) for sys.
// label names the kind of generation in help text, e.g. "NixOS". verboseCount
// points at the root command's -v counter and is read when an action runs.
func Command(sys generation.System, label string, verboseCount *int) *cli.Command {
	return &cli.Command{
		Name:        "generations",
		Aliases:     []string{"gen"},
		Usage:       fmt.Sprintf("Work with %s generations", label),
		Description: fmt.Sprintf("Work with %s generations", label),
		Commands: []*cli.Command{
			// List
			{
				Name:        "list",
				Aliases:     []string{"ls"},
				Usage:       fmt.Sprintf("List %s generations", label),
				Description: fmt.Sprintf("List %s generations", label),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					util.InitLogger(*verboseCount)
					return List(ctx, sys, cmd.String("target"))
				},
			},

			// Clean
			{
				Name:        "clean",
				Aliases:     []string{"c"},
				Usage:       fmt.Sprintf("Delete and garbage collect %s generations", label),
				Description: fmt.Sprintf("Delete and garbage collect %s generations", label),
				Flags: []cli.Flag{
					&cli.UintFlag{
						Name:    "keep",
						Aliases: []string{"k"},
						Usage:   "Number of generations to keep",
						Value:   1,
					},
					&cli.BoolFlag{
						Name:    "confirm",
						Aliases: []string{"c"},
						Usage:   "Do not ask for confirmation",
					},
					&cli.IntFlag{
						Name:  "from",
						Usage: "Lowest generation number to delete",
					},
					&cli.IntFlag{
						Name:  "to",
						Usage: "Highest generation number to delete",
					},
					&cli.BoolFlag{
						Name:  "skip-gc",
						Usage: "Skip garbage collection",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					util.InitLogger(*verboseCount)
					return cleanAction(ctx, cmd, sys)
				},
			},

			// Rollback
			{
				Name:        "rollback",
				Aliases:     []string{"rb"},
				Usage:       "Roll back to a previous generation",
				Description: "Roll back to a previous generation. Defaults to the previous generation if no ID is given.",
				ArgsUsage:   "[ID]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "confirm",
						Aliases: []string{"c"},
						Usage:   "Do not ask for confirmation",
					},
					&cli.BoolFlag{
						Name:  "cleanup",
						Usage: "Clean up newer generations after rollback",
					},
					&cli.BoolFlag{
						Name:  "skip-gc",
						Usage: "Skip garbage collection",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					util.InitLogger(*verboseCount)
					return rollbackAction(ctx, cmd, sys)
				},
			},
		},
	}
}

func cleanAction(ctx context.Context, cmd *cli.Command, sys generation.System) error {
	var from, to *int
	if cmd.IsSet("from") {
		v := int(cmd.Int("from"))
		from = &v
	}
	if cmd.IsSet("to") {
		v := int(cmd.Int("to"))
		to = &v
	}

	return Clean(ctx, sys, cmd.String("target"), CleanOptions{
		Keep:    uint(cmd.Uint("keep")),
		KeepSet: cmd.IsSet("keep"),
		From:    from,
		To:      to,
		Confirm: cmd.Bool("confirm"),
		SkipGC:  cmd.Bool("skip-gc"),
	})
}

func rollbackAction(ctx context.Context, cmd *cli.Command, sys generation.System) error {
	var id *int
	if cmd.Args().Len() > 0 {
		v, err := strconv.Atoi(cmd.Args().First())
		if err != nil {
			return fmt.Errorf("invalid generation ID: %s", cmd.Args().First())
		}
		id = &v
	}

	return Rollback(ctx, sys, cmd.String("target"), RollbackOptions{
		ID:      id,
		Confirm: cmd.Bool("confirm"),
		Cleanup: cmd.Bool("cleanup"),
		SkipGC:  cmd.Bool("skip-gc"),
	})
}
