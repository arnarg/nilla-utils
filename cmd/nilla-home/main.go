package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/urfave/cli/v3"
)

var version = "unknown"

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
	Name:    "nilla-home",
	Version: version,
	Usage:   "A nilla cli plugin to work with home-manager configurations.",
	Commands: []*cli.Command{
		// Build
		{
			Name:        "build",
			Usage:       "Build Home Manager configuration",
			Description: "Build Home Manager configuration",
			ArgsUsage:   "[configuration name]",
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
			Description: "Build Home Manager configuration and activate it",
			ArgsUsage:   "[system name]",
			Action:      actionFuncFor(subCmdSwitch),
		},
	},
}

func printSection(text string) {
	fmt.Fprintf(os.Stderr, "\033[32m>\033[0m %s\n", text)
}

func inferNames(name string) ([]string, error) {
	if name == "" {
		names := []string{}

		user := os.Getenv("USER")
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

func findHomeConfiguration(names []string) (string, error) {
	for _, name := range names {
		code := fmt.Sprintf("x: x ? \"%s\"", name)
		out, err := exec.Command(
			"nix", "eval", "-f", "nilla.nix", "systems.home", "--apply", code,
		).Output()
		if err != nil {
			continue
		}
		if string(bytes.TrimSpace(out)) == "true" {
			return name, nil
		}
	}
	return "", errHomeConfigurationNotFound
}

func findCurrentGeneration() (string, error) {
	// Check in /nix/var/nix/profiles
	if user := os.Getenv("USER"); user != "" {
		perUser := fmt.Sprintf("/nix/car/nix/profiles/per-user/%s/home-manager", user)
		if _, err := os.Stat(perUser); err == nil {
			return perUser, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}

	// Check ~/.local/state/nix/profiles
	if home := os.Getenv("HOME"); home != "" {
		homeProfile := fmt.Sprintf("%s/.local/state/nix/profiles/home-manager", home)
		if _, err := os.Stat(homeProfile); err == nil {
			return homeProfile, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", errHomeCurrentGenNotFound
}

func run(ctx context.Context, cmd *cli.Command, sc subCmd) error {
	// Try to find current generation
	current, err := findCurrentGeneration()
	if err != nil {
		return err
	}

	// Try to infer names to try for the home-manager configuration
	names, err := inferNames(cmd.Args().First())
	if err != nil {
		return err
	}

	// Find home configuration from candidates
	name, err := findHomeConfiguration(names)
	if err != nil {
		return err
	}

	// Attribute of home-manager's activation package
	attr := fmt.Sprintf("systems.home.%s.result.config.home.activationPackage", name)

	//
	// Home Manager configuration build
	//
	// Build args for nix build
	nargs := []string{"-f", "nilla.nix", attr}

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

	// Run nvd diff
	diff := exec.Command("nvd", "diff", current, string(out))
	diff.Stderr = os.Stderr
	diff.Stdout = os.Stderr
	if err := diff.Run(); err != nil {
		return err
	}

	//
	// Activate Home Manager configuration
	//
	if sc == subCmdSwitch {
		fmt.Fprintln(os.Stderr)
		printSection("Activating configuration")

		// Run switch_to_configuration
		switchp := fmt.Sprintf("%s/activate", out)
		switchc := exec.Command(switchp)
		switchc.Stderr = os.Stderr
		switchc.Stdout = os.Stdout

		if err := switchc.Run(); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
