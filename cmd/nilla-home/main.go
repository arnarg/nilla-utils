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

var verboseCount int

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
	var builder, target exec.Executor

	// Setup logger
	util.InitLogger(verboseCount)

	// Resolve project
	source, err := project.Resolve(cmd.String("project"))
	if err != nil {
		return err
	}

	// Setup builder, which is always local
	builder = exec.NewLocalExecutor()

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

	// Determine build location
	var buildTarget string
	if cmd.String("build-on") != "" {
		buildTarget = cmd.String("build-on")
	} else if cmd.Bool("build-on-target") {
		buildTarget = cmd.String("target")
		if buildTarget == "" {
			return fmt.Errorf("--build-on-target requires --target to be specified")
		}
	}

	// Validate and prepare remote build if enabled
	var storeAddress string
	if buildTarget != "" {
		user, hostname := util.ParseTarget(buildTarget)
		storeAddress = util.BuildStoreAddress(user, hostname)
		log.Debugf("Remote build enabled, target: %s, store: %s", buildTarget, storeAddress)
	}


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

	// Handle remote builds
	if buildTarget != "" && storeAddress != "" {
		// First, get the derivation path by evaluating the attribute
		// We use nix eval to get the derivation path
		printSection("Getting derivation path")
		derivationAttr := fmt.Sprintf("%s.drvPath", attr)
		evalArgs := []string{
			"-f", source.FullNillaPath(),
			derivationAttr,
			"--raw",
		}
		evalCmd := nix.Command("eval").
			Args(evalArgs).
			Executor(builder)
		derivationPath, err := evalCmd.Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to get derivation path: %w", err)
		}
		derivationPathStr := strings.TrimSpace(string(derivationPath))

		// Copy derivation to remote host
		printSection("Copying derivation to remote host")
		copyArgs := []string{
			"--to", storeAddress,
			"--derivation",
			"-s", // fetch dependencies from target system's substituters
			derivationPathStr,
		}
		copyCmd := nix.Command("copy").
			Args(copyArgs).
			Executor(builder)
		if _, err := copyCmd.Run(ctx); err != nil {
			return fmt.Errorf("failed to copy derivation to remote: %w", err)
		}

		// Add remote build flags
		nargs = append(nargs, "--store", storeAddress)
		nargs = append(nargs, "--eval-store", "auto")
	}

	// Run nix build
	printSection("Building configuration")
	nixBuildCmd := nix.Command("build").
		Args(nargs)

	if !cmd.Bool("raw") {
		nixBuildCmd = nixBuildCmd.Reporter(tui.NewBuildReporter(cmd.Bool("verbose")))
	}

	out, err := nixBuildCmd.Run(ctx)
	if err != nil {
		log.Debugf("Nix build command failed with error: %v", err)
		return fmt.Errorf("failed to build configuration: %w", err)
	}
	log.Debugf("Build completed successfully, output path: %s", string(out))

	//
	// Setup target executor
	//
	if cmd.String("target") != "" {
		log.Debugf("Setting up SSH executor for target: %s", cmd.String("target"))
		target, err = exec.NewSSHExecutor(cmd.String("target"))
		if err != nil {
			log.Debugf("Failed to create SSH executor: %v", err)
			return fmt.Errorf("failed to create SSH executor: %w", err)
		}
		log.Debugf("SSH executor created successfully")
	} else {
		target = builder
	}

	//
	// Run generation diff using nvd
	//
	fmt.Fprintln(os.Stderr)
	printSection("Comparing changes")

	// Determine executors and paths for diff
	newBuildExecutor, err := determineNewBuildExecutor(builder, target, buildTarget, storeAddress, cmd.String("target"))
	if err != nil {
		return err
	}
	currentExecutor, currentPath := determineCurrentGenerationExecutor(builder, target, current, name, cmd.String("target"))
	if currentPath == "" {
		remoteUsername := extractUsername(name)
		return fmt.Errorf("current Home Manager generation not found on target %s for user %s", cmd.String("target"), remoteUsername)
	}

	log.Debugf("Running diff: current=%s (executor=%T), new=%s (executor=%T)", currentPath, currentExecutor, string(out), newBuildExecutor)

	if err := diff.Execute(
		&diff.Generation{
			Path:     currentPath,
			Executor: currentExecutor,
		},
		&diff.Generation{
			Path:     string(out),
			Executor: newBuildExecutor,
		},
	); err != nil {
		log.Debugf("Diff execution failed with error: %v", err)
		return fmt.Errorf("failed to compare changes: %w", err)
	}
	log.Debugf("Diff execution completed successfully")

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

		// Determine executor for activation (use target if set, otherwise local)
		activateExecutor := builder
		if cmd.String("target") != "" {
			activateExecutor = target
		}

		// Run activation script
		switchp := fmt.Sprintf("%s/activate", string(out))
		switchc, err := activateExecutor.Command(switchp)
		if err != nil {
			return fmt.Errorf("failed to create activation command: %w", err)
		}

		switchc.SetStdin(os.Stdin)
		switchc.SetStderr(os.Stderr)
		switchc.SetStdout(os.Stdout)

		if err := switchc.Run(); err != nil {
			return err
		}
	}

	return nil
}

// determineNewBuildExecutor selects the executor for the new build based on build location and target.
func determineNewBuildExecutor(builder, target exec.Executor, buildTarget, storeAddress, targetStr string) (exec.Executor, error) {
	if buildTarget != "" && storeAddress != "" {
		// Built remotely
		if buildTarget == targetStr && targetStr != "" {
			return target, nil
		}
		// Built on different host - create executor for build host
		buildExecutor, err := exec.NewSSHExecutor(buildTarget)
		if err != nil {
			log.Debugf("Failed to create SSH executor for build target: %v", err)
			return nil, fmt.Errorf("failed to create SSH executor for build target: %w", err)
		}
		return buildExecutor, nil
	}
	if targetStr != "" {
		// Built locally but deploying to target
		return target, nil
	}
	return builder, nil
}

// determineCurrentGenerationExecutor selects the executor and path for the current generation.
// Returns (executor, path). Path will be empty if not found on target.
func determineCurrentGenerationExecutor(builder, target exec.Executor, current *generation.HomeGeneration, name, targetStr string) (exec.Executor, string) {
	if targetStr == "" {
		// No target - use local
		return builder, current.Path()
	}

	// Target is set - query on target
	remoteUsername := extractUsername(name)
	remotePath, found := generation.CurrentHomeGenerationPath(target, remoteUsername)
	if found {
		return target, remotePath
	}
	return target, "" // Path not found
}

// extractUsername extracts username from Home Manager system name (e.g., "root@host1" -> "root").
func extractUsername(name string) string {
	if strings.Contains(name, "@") {
		return strings.Split(name, "@")[0]
	}
	return name
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
