package main

import (
	"context"
	"fmt"
	"os"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/askpass"
	"github.com/arnarg/nilla-utils/internal/deploy"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/project"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/urfave/cli/v3"
)

var version = "unknown"

var description = `[name]  Name of the home-manager system to build. If left empty it will try "$USER@<hostname>" and "$USER".`

var verboseCount int

func actionFuncFor(sc deploy.Command) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		return run(ctx, cmd, sc)
	}
}

var app = &cli.Command{
	Name:                   "nilla-home",
	Version:                version,
	Usage:                  "A nilla cli plugin to work with home-manager configurations.",
	HideVersion:            true,
	HideHelpCommand:        true,
	UseShortOptionHandling: true,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:        "version",
			Aliases:     []string{"V"},
			Usage:       "Print version",
			HideDefault: true,
			Local:       true,
		},
		&cli.BoolFlag{
			Name:        "verbose",
			Aliases:     []string{"v"},
			Usage:       "Set log level to verbose (pass multiple times, e.g. -vv for SSH debug)",
			HideDefault: true,
			Config: cli.BoolConfig{
				Count: &verboseCount,
			},
		},
		&cli.BoolFlag{
			Name:  "compact",
			Usage: "Make build and copy progress view more compact",
		},
		&cli.BoolFlag{
			Name:  "raw",
			Usage: "Raw output from Nix",
		},
		&cli.StringFlag{
			Name:    "project",
			Aliases: []string{"p"},
			Usage:   "The nilla project to use",
			Value:   "./",
		},
		&cli.StringFlag{
			Name:    "target",
			Aliases: []string{"t"},
			Usage:   "Target host to deploy/activate on (for switch command). Can also be used with --build-on-target for builds.",
		},
		&cli.StringFlag{
			Name:  "build-on",
			Usage: "Build on the specified host instead of locally. Dependencies are fetched from target's substituters.",
		},
		&cli.BoolFlag{
			Name:  "build-on-target",
			Usage: "Build on the same host as specified by --target (requires --target flag). Dependencies are fetched from target's substituters.",
		},
	},
	Commands: []*cli.Command{
		// Build
		{
			Name:        "build",
			Usage:       "Build Home Manager configuration",
			Description: fmt.Sprintf("Build Home Manager configuration.\n\n%s", description),
			ArgsUsage:   "[name]",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "no-link",
					Usage: "Do not create symlinks to the build results",
				},
				&cli.BoolFlag{
					Name:  "print-out-paths",
					Usage: "Print the resulting output paths",
				},
				&cli.StringFlag{
					Name:    "out-link",
					Aliases: []string{"o"},
					Usage:   "Use path as prefix for the symlinks to the build results",
				},
			},
			Action: actionFuncFor(deploy.Build),
		},

		// Switch
		{
			Name:        "switch",
			Usage:       "Build Home Manager configuration and activate it",
			Description: fmt.Sprintf("Build Home Manager configuration and activate it.\n\n%s", description),
			ArgsUsage:   "[name]",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
			},
			Action: actionFuncFor(deploy.Switch),
		},

		// List
		{
			Name:        "list",
			Aliases:     []string{"ls"},
			Usage:       "List Home Manager configurations in project",
			Description: "List Home Manager configurations in project",
			Action:      listConfigurations,
		},

		// Generations
		{
			Name:        "generations",
			Aliases:     []string{"gen"},
			Usage:       "Work with home-manager generations",
			Description: "Work with home-manager generations",
			Commands: []*cli.Command{
				// List
				{
					Name:        "list",
					Aliases:     []string{"ls"},
					Usage:       "List home-manager generations",
					Description: "List home-manager generations",
					Action:      listGenerations,
				},

				// Clean
				{
					Name:        "clean",
					Aliases:     []string{"c"},
					Usage:       "Delete and garbage collect NixOS generations",
					Description: "Delete and garbage collect NixOS generations",
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
					},
					Action: cleanGenerations,
				},
			},
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if socket := os.Getenv("NILLA_ASKPASS_SOCKET"); socket != "" {
			commandID := os.Getenv("NILLA_ASKPASS_COMMAND_ID")
			var prompt string
			if len(os.Args) > 1 {
				prompt = os.Args[len(os.Args)-1]
			}
			host := askpass.ParseHostFromPrompt(prompt)
			password, err := askpass.GetPassword(socket, host, commandID)
			if err != nil {
				return err
			}
			fmt.Println(password)
			return nil
		}

		if cmd.Args().Len() < 1 {
			cli.ShowAppHelp(cmd)
		}
		if cmd.Bool("version") {
			cli.ShowVersion(cmd)
		}
		return nil
	},
}

func printSection(text string) {
	fmt.Fprintf(os.Stderr, "\033[32m>\033[0m %s\n", text)
}

func run(ctx context.Context, cmd *cli.Command, sc deploy.Command) error {
	util.InitLogger(verboseCount)

	plan, err := deploy.ResolvePlan(deploy.Options{
		ProjectPath: cmd.String("project"),
		Name:        cmd.Args().First(),
		SubCmd:      sc,
		BuildOn:     cmd.String("build-on"),
		BuildOnSelf: cmd.Bool("build-on-target"),
		Target:      cmd.String("target"),
		Raw:         cmd.Bool("raw"),
		Verbose:     cmd.Bool("verbose"),
		Compact:     cmd.Bool("compact"),
		NoLink:      cmd.Bool("no-link"),
		OutLink:     cmd.String("out-link"),
		Confirm:     cmd.Bool("confirm"),
	}, deploy.HomeSystem{})
	if err != nil {
		return err
	}

	s, err := deploy.NewSession(ctx, plan, deploy.HomeSystem{}, deploy.DefaultDeps())
	if err != nil {
		return err
	}
	defer s.Close()

	return s.Run(ctx)
}

func listConfigurations(ctx context.Context, cmd *cli.Command) error {
	// Setup logger
	util.InitLogger(verboseCount)

	// Resolve project
	source, err := project.Resolve(cmd.String("project"))
	if err != nil {
		return err
	}

	// Get a list of home systems
	systems, err := nix.ListAttrsInProject(source.NillaPath, source.FixedOutputStoreEntry(), "systems.home")
	if err != nil {
		return err
	}

	// Print results
	if len(systems) < 1 {
		fmt.Println("No Home Manager configurations found")
	} else {
		printSection("Home Manager configurations")
		for _, system := range systems {
			fmt.Printf("- %s\n", system)
		}
	}

	return nil
}

func main() {
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}
