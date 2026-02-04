package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/project"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v3"
)

var version = "unknown"

var description = `[name]  Name of the MicroVM to manage.`

var verboseCount int

const (
	stateDir       = "/var/lib/microvms"
	gcrootsDir     = "/nix/var/nix/gcroots/microvm"
	systemdService = "microvm"
)

var app = &cli.Command{
	Name:                   "nilla-microvm",
	Version:                version,
	Usage:                  "A nilla cli plugin to work with MicroVM configurations.",
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
	},
	Commands: []*cli.Command{
		// Create
		{
			Name:        "create",
			Usage:       "Create a MicroVM",
			Description: fmt.Sprintf("Create a MicroVM.\n\n%s", description),
			ArgsUsage:   "<name>",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
			},
			Action: createMicroVM,
		},

		// Update
		{
			Name:        "update",
			Usage:       "Update a MicroVM",
			Description: fmt.Sprintf("Rebuild and update a MicroVM.\n\n%s", description),
			ArgsUsage:   "<name>",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
				&cli.BoolFlag{
					Name:    "restart",
					Aliases: []string{"r"},
					Usage:   "Restart the MicroVM after update",
				},
			},
			Action: updateMicroVM,
		},

		// List
		{
			Name:        "list",
			Aliases:     []string{"ls"},
			Usage:       "List MicroVMs in project",
			Description: "List MicroVMs in project",
			Action:      listMicroVMs,
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

// getMicroVMAttr returns the nix attribute path for a microvm
func getMicroVMAttr(name string) string {
	return fmt.Sprintf("systems.microvm.\"%s\".result.config.microvm.declaredRunner", name)
}

// getMicroVMStateDir returns the state directory for a microvm
func getMicroVMStateDir(name string) string {
	return filepath.Join(stateDir, name)
}

// buildMicroVM builds the microvm and returns the output path
func buildMicroVM(ctx context.Context, cmd *cli.Command, name string) (string, error) {
	// Resolve project
	source, err := project.Resolve(cmd.String("project"))
	if err != nil {
		return "", err
	}

	// Setup builder
	builder := exec.NewLocalExecutor()

	// Attribute of MicroVM declaredRunner
	attr := getMicroVMAttr(name)

	// Check if attribute exists
	exists, err := nix.ExistsInProject(source.NillaPath, source.FixedOutputStoreEntry(), attr)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("attribute '%s' does not exist in project \"%s\"", attr, source.FullNillaPath())
	}

	log.Infof("Found MicroVM \"%s\"", name)

	// Build args for nix build
	nargs := []string{"-f", source.FullNillaPath(), attr, "--no-link"}

	// Run nix build
	printSection("Building MicroVM")
	nixBuildCmd := nix.Command("build").
		Args(nargs).
		Executor(builder)

	if !cmd.Bool("raw") {
		nixBuildCmd = nixBuildCmd.Reporter(tui.NewBuildReporter(cmd.Bool("verbose")))
	}

	out, err := nixBuildCmd.Run(ctx)
	if err != nil {
		log.Debugf("Nix build command failed with error: %v", err)
		return "", fmt.Errorf("failed to build MicroVM: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func createMicroVM(ctx context.Context, cmd *cli.Command) error {
	// Setup logger
	util.InitLogger(verboseCount)

	// Get microvm name
	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("MicroVM name is required")
	}

	// Check if already exists
	stateDir := getMicroVMStateDir(name)
	if _, err := os.Stat(stateDir); err == nil {
		return fmt.Errorf("MicroVM \"%s\" already exists at %s", name, stateDir)
	}

	// Build the microvm
	outPath, err := buildMicroVM(ctx, cmd, name)
	if err != nil {
		return err
	}

	log.Debugf("Build completed successfully, output path: %s", outPath)

	// Ask confirmation
	if !cmd.Bool("confirm") {
		doContinue, err := tui.RunConfirm(fmt.Sprintf("Create MicroVM \"%s\"?", name))
		if err != nil {
			return err
		}
		if !doContinue {
			return nil
		}
	}

	// Create state directory
	printSection("Creating MicroVM state directory")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Create current symlink
	currentLink := filepath.Join(stateDir, "current")
	if err := os.Symlink(outPath, currentLink); err != nil {
		return fmt.Errorf("failed to create current symlink: %w", err)
	}

	// Create gcroots directory
	if err := os.MkdirAll(gcrootsDir, 0755); err != nil {
		return fmt.Errorf("failed to create gcroots directory: %w", err)
	}

	// Create gcroot for current
	gcrootCurrent := filepath.Join(gcrootsDir, name)
	if err := os.Symlink(currentLink, gcrootCurrent); err != nil {
		return fmt.Errorf("failed to create gcroot for current: %w", err)
	}

	// Create gcroot for booted (points to current initially)
	gcrootBooted := filepath.Join(gcrootsDir, "booted-"+name)
	if err := os.Symlink(currentLink, gcrootBooted); err != nil {
		return fmt.Errorf("failed to create gcroot for booted: %w", err)
	}

	// Set permissions
	if err := os.Chown(stateDir, -1, 0); err != nil {
		log.Warnf("Failed to set group ownership: %v", err)
	}

	fmt.Fprintf(os.Stderr, "\n\033[32mCreated MicroVM %s.\033[0m Start with: \033[1;36msystemctl start %s@%s.service\033[0m\n",
		name, systemdService, name)

	return nil
}

func updateMicroVM(ctx context.Context, cmd *cli.Command) error {
	// Setup logger
	util.InitLogger(verboseCount)

	// Get microvm name
	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("MicroVM name is required")
	}

	// Check if exists
	stateDir := getMicroVMStateDir(name)
	if _, err := os.Stat(stateDir); err != nil {
		return fmt.Errorf("MicroVM \"%s\" does not exist at %s", name, stateDir)
	}

	// Check if it's declarative (has toplevel file)
	toplevelPath := filepath.Join(stateDir, "toplevel")
	if _, err := os.Stat(toplevelPath); err == nil {
		return fmt.Errorf("this MicroVM is managed fully declaratively and cannot be updated manually")
	}

	// Build the microvm
	outPath, err := buildMicroVM(ctx, cmd, name)
	if err != nil {
		return err
	}

	log.Debugf("Build completed successfully, output path: %s", outPath)

	// Get current path for diff
	currentLink := filepath.Join(stateDir, "current")
	var oldPath string
	if fi, err := os.Lstat(currentLink); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		oldPath, _ = os.Readlink(currentLink)
	}

	// Show diff if we have an old path
	if oldPath != "" && oldPath != outPath {
		fmt.Fprintln(os.Stderr)
		printSection("Comparing changes")

		builder := exec.NewLocalExecutor()
		if err := diffClosures(ctx, builder, oldPath, outPath); err != nil {
			log.Warnf("Failed to show diff: %v", err)
		}
	}

	// Ask confirmation
	if !cmd.Bool("confirm") {
		doContinue, err := tui.RunConfirm(fmt.Sprintf("Update MicroVM \"%s\"?", name))
		if err != nil {
			return err
		}
		if !doContinue {
			return nil
		}
	}

	// Update current symlink
	printSection("Updating MicroVM")

	// Remove old current symlink
	if err := os.Remove(currentLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old current symlink: %w", err)
	}

	// Create new current symlink
	if err := os.Symlink(outPath, currentLink); err != nil {
		return fmt.Errorf("failed to create new current symlink: %w", err)
	}

	// Check if booted exists and compare
	bootedLink := filepath.Join(stateDir, "booted")
	var bootedPath string
	if fi, err := os.Lstat(bootedLink); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		bootedPath, _ = os.Readlink(bootedLink)
	}

	if bootedPath != "" {
		if bootedPath == outPath {
			fmt.Fprintf(os.Stderr, "No reboot of MicroVM %s required.\n", name)
		} else if cmd.Bool("restart") {
			fmt.Fprintf(os.Stderr, "Rebooting MicroVM %s\n", name)
			if err := restartMicroVM(ctx, name); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(os.Stderr, "Reboot MicroVM %s for the new profile: systemctl restart %s@%s.service\n",
				name, systemdService, name)
		}
	} else if cmd.Bool("restart") {
		fmt.Fprintf(os.Stderr, "Booting MicroVM %s\n", name)
		if err := restartMicroVM(ctx, name); err != nil {
			return err
		}
	}

	return nil
}

func diffClosures(ctx context.Context, exec exec.Executor, oldPath, newPath string) error {
	cmd, err := exec.CommandContext(ctx, "nix", "store", "diff-closures", oldPath, newPath)
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func restartMicroVM(ctx context.Context, name string) error {
	builder := exec.NewLocalExecutor()
	cmd, err := builder.CommandContext(ctx, "systemctl", "restart", fmt.Sprintf("%s@%s.service", systemdService, name))
	if err != nil {
		return err
	}
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func listMicroVMs(ctx context.Context, cmd *cli.Command) error {
	// Setup logger
	util.InitLogger(verboseCount)

	// Resolve project
	source, err := project.Resolve(cmd.String("project"))
	if err != nil {
		return err
	}

	// Get a list of microvms
	systems, err := nix.ListAttrsInProject(source.NillaPath, source.FixedOutputStoreEntry(), "systems.microvm")
	if err != nil {
		return err
	}

	// Print results
	if len(systems) < 1 {
		fmt.Println("No MicroVM configurations found")
	} else {
		printSection("MicroVM configurations")
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
