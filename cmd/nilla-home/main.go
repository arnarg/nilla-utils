package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	gexec "os/exec"
	"strings"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/project"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v3"
)

var version = "unknown"

var description = `[name]  Name of the home-manager system to build. If left empty it will try "$USER@<hostname>" and "$USER".`

type subCmd int

const (
	subCmdBuild subCmd = iota
	subCmdSwitch
)

var (
	errNoUserFound               = errors.New("no user found")
	errHomeConfigurationNotFound = errors.New("home configuration not found")
	errHomeCurrentGenNotFound    = errors.New("current generation not found")
)

func actionFuncFor(sub subCmd) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		return run(ctx, cmd, sub)
	}
}

var app = &cli.Command{
	Name:            "nilla-home",
	Version:         version,
	Usage:           "A nilla cli plugin to work with home-manager configurations.",
	HideVersion:     true,
	HideHelpCommand: true,
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
			Usage:       "Set log level to verbose",
			HideDefault: true,
		},
		&cli.StringFlag{
			Name:    "project",
			Aliases: []string{"p"},
			Usage:   "The nilla project to use",
			Value:   "./",
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
			Action: actionFuncFor(subCmdBuild),
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
			Action: actionFuncFor(subCmdSwitch),
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

func inferNames(name string) ([]string, error) {
	if name == "" {
		names := []string{}

		user := util.GetUser()
		if user == "" {
			return nil, errNoUserFound
		}

		if hn, err := os.Hostname(); err == nil {
			names = append(names, fmt.Sprintf("%s@%s", user, hn))
		}

		return append(names, user), nil
	}
	return []string{name}, nil
}

func findHomeConfiguration(p string, names []string) (string, error) {
	for _, name := range names {
		code := fmt.Sprintf("x: x ? \"%s\"", name)
		out, err := gexec.Command(
			"nix", "eval", "-f", p, "systems.home", "--apply", code,
		).Output()
		if err != nil {
			continue
		}
		if string(bytes.TrimSpace(out)) == "true" {
			return name, nil
		}
	}
	return "", fmt.Errorf("Home configurations \"%s\" not found", strings.Join(names, ", "))
}

func run(ctx context.Context, cmd *cli.Command, sc subCmd) error {
	// Setup logger
	util.InitLogger(cmd.Bool("verbose"))

	// Resolve project
	source, err := project.Resolve(cmd.String("project"))
	if err != nil {
		return err
	}

	// Try to find current generation
	current, err := generation.CurrentHomeGeneration()
	if err != nil {
		return err
	}

	// Try to infer names to try for the home-manager configuration
	names, err := inferNames(cmd.Args().First())
	if err != nil {
		return err
	}

	// Find home configuration from candidates
	name, err := findHomeConfiguration(source.FullNillaPath(), names)
	if err != nil {
		return err
	}

	// Attribute of home-manager's activation package
	attr := fmt.Sprintf("systems.home.\"%s\".result.config.home.activationPackage", name)

	// Check if attribute exists
	exists, err := nix.ExistsInProject(source.NillaPath, source.FixedOutputStoreEntry(), attr)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Attribute '%s' does not exist in project \"%s\"", attr, source.FullNillaPath())
	}

	log.Infof("Found system \"%s\"", name)

	// Setup builder, which is always local
	builder := exec.NewLocalExecutor()

	//
	// Home Manager configuration build
	//
	// Build args for nix build
	nargs := []string{"-f", source.FullNillaPath(), attr}

	// Add extra args depending on the sub command
	if sc == subCmdBuild {
		if cmd.Bool("no-link") {
			nargs = append(nargs, "--no-link")
		}
		if cmd.String("out-link") != "" {
			nargs = append(nargs, "--out-link", cmd.String("out-link"))
		}
	} else {
		// All sub-commands except build should not
		// create a result link
		nargs = append(nargs, "--no-link")
	}

	// Run nix build
	printSection("Building configuration")
	out, err := nix.Command("build").
		Args(nargs).
		Reporter(tui.NewBuildReporter(cmd.Bool("verbose"))).
		Run(ctx)
	if err != nil {
		return err
	}

	//
	// Run generation diff using nvd
	//
	fmt.Fprintln(os.Stderr)
	printSection("Comparing changes")

	if err := diff.Execute(
		&diff.Generation{
			Path:     current.Path(),
			Executor: builder,
		},
		&diff.Generation{
			Path:     string(out),
			Executor: builder,
		},
	); err != nil {
		return err
	}

	// Build can exit now
	if sc == subCmdBuild {
		return nil
	}

	//
	// Ask Confirmation
	//
	if !cmd.Bool("confirm") {
		doContinue, err := tui.RunConfirm("Do you want to continue?")
		if err != nil {
			return err
		}
		if !doContinue {
			return nil
		}
	}

	//
	// Activate Home Manager configuration
	//
	if sc == subCmdSwitch {
		fmt.Fprintln(os.Stderr)
		printSection("Activating configuration")

		// Run switch_to_configuration
		switchp := fmt.Sprintf("%s/activate", out)
		switchc := gexec.Command(switchp)
		switchc.Stderr = os.Stderr
		switchc.Stdout = os.Stdout

		if err := switchc.Run(); err != nil {
			return err
		}
	}

	return nil
}

func listConfigurations(ctx context.Context, cmd *cli.Command) error {
	// Setup logger
	util.InitLogger(cmd.Bool("verbose"))

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
