package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arnarg/nilla-utils/internal/diff"
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
		// Install
		{
			Name:        "install",
			Usage:       "Install a MicroVM",
			Description: fmt.Sprintf("Install a MicroVM.\n\n%s", description),
			ArgsUsage:   "<name>",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
			},
			Action: installMicroVM,
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

		// Uninstall
		{
			Name:        "uninstall",
			Usage:       "Uninstall a MicroVM",
			Description: fmt.Sprintf("Uninstall a MicroVM.\n\n%s", description),
			ArgsUsage:   "<name>",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
			},
			Action: uninstallMicroVM,
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

// getMicroVMAttr returns the nix attribute path for a microvm activation package
func getMicroVMAttr(name string) string {
	return fmt.Sprintf("systems.microvm.\"%s\".result.config.microvm.activationPackage", name)
}

// getMicroVMStateDir returns the state directory for a microvm
func getMicroVMStateDir(name string) string {
	return filepath.Join(stateDir, name)
}

// buildActivationPackage builds the microvm activation package and returns the output path
func buildActivationPackage(ctx context.Context, cmd *cli.Command, name string) (string, error) {
	// Resolve project
	source, err := project.Resolve(cmd.String("project"))
	if err != nil {
		return "", err
	}

	// Setup builder
	builder := exec.NewLocalExecutor()

	// Attribute of MicroVM activationPackage
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

// getDeclaredRunnerPath returns the path to the declared-runner within the activation package
func getDeclaredRunnerPath(activationPkg string) string {
	return filepath.Join(activationPkg, "declared-runner")
}

// getManageVMPath returns the path to the manage-vm script within the activation package
func getManageVMPath(activationPkg string) string {
	return filepath.Join(activationPkg, "bin", "manage-vm")
}

func installMicroVM(ctx context.Context, cmd *cli.Command) error {
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

	// Build the activation package
	activationPkg, err := buildActivationPackage(ctx, cmd, name)
	if err != nil {
		return err
	}

	log.Debugf("Build completed successfully, activation package: %s", activationPkg)

	// Ask confirmation
	if !cmd.Bool("confirm") {
		doContinue, err := tui.RunConfirm(fmt.Sprintf("Install MicroVM \"%s\"?", name))
		if err != nil {
			return err
		}
		if !doContinue {
			return nil
		}
	}

	// Run manage-vm install with sudo
	fmt.Fprintln(os.Stderr)
	printSection("Installing MicroVM")
	manageVM := getManageVMPath(activationPkg)
	localExec := exec.NewLocalExecutor()
	installCmd, err := localExec.Command("sudo", manageVM, "install")
	if err != nil {
		return fmt.Errorf("failed to create install command: %w", err)
	}
	installCmd.SetStdin(os.Stdin)
	installCmd.SetStderr(os.Stderr)
	installCmd.SetStdout(os.Stdout)
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install MicroVM: %w", err)
	}

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
		return fmt.Errorf("This MicroVM is managed fully declaratively and cannot be updated manually")
	}

	// Build the activation package
	activationPkg, err := buildActivationPackage(ctx, cmd, name)
	if err != nil {
		return err
	}

	// Get the new declared runner path
	newRunnerPath := getDeclaredRunnerPath(activationPkg)
	log.Debugf("Build completed successfully, activation package: %s", activationPkg)

	// Get current path for diff
	currentLink := filepath.Join(stateDir, "current")
	var oldPath string
	if fi, err := os.Lstat(currentLink); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		oldPath, _ = os.Readlink(currentLink)
	}

	// Show diff if we have an old path
	if oldPath != "" && oldPath != newRunnerPath {
		// Get the system paths for comparison
		oldSystemPath := filepath.Join(oldPath, "share", "microvm", "system")
		newSystemPath := filepath.Join(newRunnerPath, "share", "microvm", "system")

		// Check both system paths exist and are symlinks
		oldFi, oldErr := os.Lstat(oldSystemPath)
		newFi, newErr := os.Lstat(newSystemPath)

		oldExists := oldErr == nil && oldFi.Mode()&os.ModeSymlink != 0
		newExists := newErr == nil && newFi.Mode()&os.ModeSymlink != 0

		if oldExists && newExists {
			fmt.Fprintln(os.Stderr)
			printSection("Comparing changes")

			localExec := exec.NewLocalExecutor()
			if err := diff.Execute(
				&diff.Generation{
					Path:     oldSystemPath,
					Executor: localExec,
				},
				&diff.Generation{
					Path:     newSystemPath,
					Executor: localExec,
				},
			); err != nil {
				log.Warnf("Failed to show diff: %v", err)
			}
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

	// Run manage-vm update with sudo
	fmt.Fprintln(os.Stderr)
	printSection("Updating MicroVM")
	manageVM := getManageVMPath(activationPkg)
	localExec := exec.NewLocalExecutor()
	updateArgs := []string{manageVM, "update"}
	if cmd.Bool("restart") {
		updateArgs = append(updateArgs, "--restart")
	}
	updateCmd, err := localExec.Command("sudo", updateArgs...)
	if err != nil {
		return fmt.Errorf("failed to create update command: %w", err)
	}
	updateCmd.SetStdin(os.Stdin)
	updateCmd.SetStderr(os.Stderr)
	updateCmd.SetStdout(os.Stdout)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to update MicroVM: %w", err)
	}

	return nil
}

func uninstallMicroVM(ctx context.Context, cmd *cli.Command) error {
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
		return fmt.Errorf("This MicroVM is managed fully declaratively and cannot be updated manually")
	}

	log.Infof("MicroVM %s found.", name)

	// Check if uninstall script exists
	uninstallScript := filepath.Join(stateDir, "uninstall")
	if _, err := os.Stat(uninstallScript); err != nil {
		log.Warnf("Uninstall script not found at %s. Manual cleanup is needed.", uninstallScript)
		return fmt.Errorf("uninstall script not found")
	}

	// Ask confirmation
	if !cmd.Bool("confirm") {
		doContinue, err := tui.RunConfirm(fmt.Sprintf("Uninstall MicroVM \"%s\"?", name))
		if err != nil {
			return err
		}
		if !doContinue {
			return nil
		}
	}

	// Run uninstall script with sudo
	fmt.Fprintln(os.Stderr)
	printSection("Uninstalling MicroVM")
	localExec := exec.NewLocalExecutor()
	uninstallCmd, err := localExec.Command("sudo", uninstallScript)
	if err != nil {
		return fmt.Errorf("failed to create uninstall command: %w", err)
	}
	uninstallCmd.SetStdin(os.Stdin)
	uninstallCmd.SetStderr(os.Stderr)
	uninstallCmd.SetStdout(os.Stdout)
	if err := uninstallCmd.Run(); err != nil {
		return fmt.Errorf("failed to uninstall MicroVM: %w", err)
	}

	return nil
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
