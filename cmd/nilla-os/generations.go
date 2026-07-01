package main

import (
	"context"

	"github.com/arnarg/nilla-utils/internal/gencmd"
	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/urfave/cli/v3"
)

func listGenerations(ctx context.Context, cmd *cli.Command) error {
	util.InitLogger(verboseCount)
	return gencmd.List(ctx, generation.NixOSSystem{}, cmd.String("target"))
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

	return gencmd.Clean(ctx, generation.NixOSSystem{}, cmd.String("target"), gencmd.CleanOptions{
		Keep:    uint(cmd.Uint("keep")),
		KeepSet: cmd.IsSet("keep"),
		From:    from,
		To:      to,
		Confirm: cmd.Bool("confirm"),
		SkipGC:  cmd.Bool("skip-gc"),
	})
}
