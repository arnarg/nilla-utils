package main

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"time"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v3"
)

var app = &cli.Command{
	Name:            "tui",
	Usage:           "Testing utility for TUI reporters.",
	HideVersion:     true,
	HideHelpCommand: true,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "reporter",
			Aliases:  []string{"r"},
			Usage:    "The reporter to test",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "data-file",
			Aliases:  []string{"f"},
			Usage:    "Test data file to test",
			Required: true,
		},
		&cli.UintFlag{
			Name:    "max-delay",
			Aliases: []string{"d"},
			Usage:   "Max number of milliseconds of delay in reading data file",
			Value:   40,
		},
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Verbose logging",
		},
	},
	Action: run,
}

type delayReader struct {
	r        io.Reader
	maxDelay uint64
}

func (r *delayReader) Read(p []byte) (int, error) {
	if r.maxDelay < 1 {
		return r.r.Read(p)
	}

	delay := time.Duration(rand.IntN(int(r.maxDelay)))
	time.Sleep(delay * time.Millisecond)
	return r.r.Read(p)
}

func run(ctx context.Context, cmd *cli.Command) error {
	rprtr := cmd.String("reporter")
	dataFile := cmd.String("data-file")
	maxDelay := cmd.Uint("max-delay")

	var reporter nix.ProgressReporter
	switch rprtr {
	case "build":
		reporter = tui.NewBuildReporter(cmd.Bool("verbose"))
	case "copy":
		reporter = tui.NewCopyReporter(cmd.Bool("verbose"))
	default:
		return errors.New("--reporter needs to be either \"build\" or \"copy\"")
	}

	data, err := os.Open(dataFile)
	if err != nil {
		return err
	}

	decoder := nix.NewProgressDecoder(&delayReader{data, maxDelay})

	return reporter.Run(ctx, decoder)
}

func main() {
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}
