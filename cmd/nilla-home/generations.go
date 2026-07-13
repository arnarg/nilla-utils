package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/arnarg/nilla-utils/internal/gencmd"
	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/urfave/cli/v3"
)

func listGenerations(ctx context.Context, cmd *cli.Command) error {
	util.InitLogger(verboseCount)
	return gencmd.List(ctx, generation.HomeSystem{}, cmd.String("target"))
}

func cleanGenerations(ctx context.Context, cmd *cli.Command) error {
	util.InitLogger(verboseCount)

	var from, to *int
	if cmd.IsSet("from") {
		v := int(cmd.Int("from"))
		from = &v
	}
	if cmd.IsSet("to") {
		v := int(cmd.Int("to"))
		to = &v
	}

	return gencmd.Clean(ctx, generation.HomeSystem{}, cmd.String("target"), gencmd.CleanOptions{
		Keep:    uint(cmd.Uint("keep")),
		KeepSet: cmd.IsSet("keep"),
		From:    from,
		To:      to,
		Confirm: cmd.Bool("confirm"),
		SkipGC:  cmd.Bool("skip-gc"),
	})
}

func rollbackGenerations(ctx context.Context, cmd *cli.Command) error {
	util.InitLogger(verboseCount)

	var id *int
	if cmd.Args().Len() > 0 {
		v, err := strconv.Atoi(cmd.Args().First())
		if err != nil {
			return fmt.Errorf("invalid generation ID: %s", cmd.Args().First())
		}
		id = &v
	}

	return gencmd.Rollback(ctx, generation.HomeSystem{}, cmd.String("target"), gencmd.RollbackOptions{
		ID:      id,
		Confirm: cmd.Bool("confirm"),
		Cleanup: cmd.Bool("cleanup"),
		SkipGC:  cmd.Bool("skip-gc"),
	})
}
