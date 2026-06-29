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
	return gencmd.List(ctx, generation.HomeSystem{}, cmd.String("target"))
}

func cleanGenerations(ctx context.Context, cmd *cli.Command) error {
	util.InitLogger(verboseCount)
	return gencmd.Clean(ctx, generation.HomeSystem{}, cmd.String("target"), uint(cmd.Uint("keep")), cmd.Bool("confirm"))
}
