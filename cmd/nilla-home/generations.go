package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"slices"
	"strconv"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/project"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/lipgloss"
	"github.com/urfave/cli/v3"
)

func sortGenerationsDesc(generations []*generation.HomeGeneration) {
	slices.SortFunc(generations, func(a, b *generation.HomeGeneration) int {
		return cmp.Compare(b.ID, a.ID)
	})
}

func setupExecutor(targetStr string) (exec.Executor, string, error) {
	if targetStr == "" {
		return nil, "", nil
	}

	executor, err := exec.NewSSHExecutor(targetStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to setup SSH executor for target %s: %w", targetStr, err)
	}

	// Extract username from target using util.ParseTarget
	username, _ := util.ParseTarget(targetStr)
	return executor, username, nil
}

func listGenerations(ctx context.Context, cmd *cli.Command) error {
	// Allow 0 or 1 argument: optional Home Manager configuration name (e.g., "root@host1")
	if err := util.ValidateArgs(cmd, 1); err != nil {
		return err
	}

	targetStr := cmd.String("target")
	configName := cmd.Args().First()

	var username string
	if configName != "" {
		// Configuration name provided - validate before connecting
		if targetStr == "" {
			return fmt.Errorf("--target is required when listing generations for a specific configuration")
		}

		// Validate configuration exists in project (before SSH connection)
		source, err := project.Resolve(cmd.String("project"))
		if err != nil {
			return err
		}
		systems, err := nix.ListAttrsInProject(source.NillaPath, source.FixedOutputStoreEntry(), "systems.home")
		if err != nil {
			return err
		}
		found := false
		for _, system := range systems {
			if system == configName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("configuration '%s' not found in project. Available configurations: %v", configName, systems)
		}

		// Extract hostname from target
		_, targetHostname := util.ParseTarget(targetStr)

		// Get configuration name from argument
		configUser, configHostname := util.ParseTarget(configName)

		// Validate hostname matches target
		if configHostname != targetHostname {
			return fmt.Errorf("hostname mismatch: configuration '%s' has hostname '%s' but --target has hostname '%s'", configName, configHostname, targetHostname)
		}

		// Use username from configuration name
		username = configUser
	}

	// Setup executor after validation (to avoid connecting if validation fails)
	executor, targetUsername, err := setupExecutor(targetStr)
	if err != nil {
		return err
	}

	if username == "" {
		// No configuration name provided - use username from target
		username = targetUsername
	}

	// Get current generation
	current, err := generation.CurrentHomeGeneration(executor, username)
	if err != nil {
		return err
	}

	// List all generations
	generations, err := generation.ListHomeGenerations(executor, username)
	if err != nil {
		return err
	}

	// Sort the list in reverse by ID
	sortGenerationsDesc(generations)

	// Build table
	headers := []string{"Generation", "Build date", "Home Manager version"}
	rows := [][]string{}
	for _, gen := range generations {
		pre := " "
		if gen.ID == current.ID {
			pre = lipgloss.NewStyle().
				Foreground(lipgloss.Color("13")).
				Bold(true).
				SetString("*").
				String()
		}

		rows = append(rows, []string{
			fmt.Sprintf("%s %d", pre, gen.ID),
			gen.BuildDate.Format(time.DateTime),
			gen.Version,
		})
	}

	fmt.Println(util.RenderTable(headers, rows...))

	return nil
}

type genAction struct {
	generation *generation.HomeGeneration
	keep       bool
}

func cleanGenerations(ctx context.Context, cmd *cli.Command) error {
	// Allow 0 or 1 argument: optional Home Manager configuration name (e.g., "root@host1")
	if err := util.ValidateArgs(cmd, 1); err != nil {
		return err
	}

	targetStr := cmd.String("target")
	configName := cmd.Args().First()

	var username string
	if configName != "" {
		// Configuration name provided - validate before connecting
		if targetStr == "" {
			return fmt.Errorf("--target is required when cleaning generations for a specific configuration")
		}

		// Validate configuration exists in project (before SSH connection)
		source, err := project.Resolve(cmd.String("project"))
		if err != nil {
			return err
		}
		systems, err := nix.ListAttrsInProject(source.NillaPath, source.FixedOutputStoreEntry(), "systems.home")
		if err != nil {
			return err
		}
		found := false
		for _, system := range systems {
			if system == configName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("configuration '%s' not found in project. Available configurations: %v", configName, systems)
		}

		// Extract hostname from target
		_, targetHostname := util.ParseTarget(targetStr)

		// Get configuration name from argument
		configUser, configHostname := util.ParseTarget(configName)

		// Validate hostname matches target
		if configHostname != targetHostname {
			return fmt.Errorf("hostname mismatch: configuration '%s' has hostname '%s' but --target has hostname '%s'", configName, configHostname, targetHostname)
		}

		// Use username from configuration name
		username = configUser
	}

	// Setup executor after validation (to avoid connecting if validation fails)
	executor, targetUsername, err := setupExecutor(targetStr)
	if err != nil {
		return err
	}

	if username == "" {
		// No configuration name provided - use username from target
		username = targetUsername
	}

	// Parse parameters
	keep := cmd.Uint("keep")
	foundCurrent := false

	// Get current generation
	current, err := generation.CurrentHomeGeneration(executor, username)
	if err != nil {
		return err
	}

	// List all generations
	generations, err := generation.ListHomeGenerations(executor, username)
	if err != nil {
		return err
	}

	// Sort the list in reverse by ID
	sortGenerationsDesc(generations)

	// Make a plan
	remaining := keep
	actions := []genAction{}
	for _, gen := range generations {
		doKeep := remaining > 0

		if gen.ID == current.ID {
			doKeep = true
			foundCurrent = true
		} else if !foundCurrent && remaining == 1 {
			doKeep = false
		}

		if doKeep {
			remaining -= 1
		}

		actions = append(actions, genAction{gen, doKeep})
	}

	// Build plan table
	headers := []string{"Generation", "Build date", "Home Manager version"}
	rows := [][]string{}
	keepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	for _, action := range actions {
		gen := action.generation
		pre := " "
		if gen.ID == current.ID {
			pre = lipgloss.NewStyle().
				Foreground(lipgloss.Color("13")).
				Bold(true).
				SetString("*").
				String()
		}

		var style lipgloss.Style
		if action.keep {
			style = keepStyle
		} else {
			style = delStyle
		}

		rows = append(rows, []string{
			fmt.Sprintf(
				"%s %s",
				pre,
				style.SetString(strconv.Itoa(gen.ID)).String(),
			),
			style.SetString(gen.BuildDate.Format(time.DateTime)).String(),
			style.SetString(gen.Version).String(),
		})
	}

	//
	// Display plan
	//
	printSection("Plan")
	fmt.Fprintln(os.Stderr, util.RenderTable(headers, rows...))

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
	// Delete generation links
	//
	for _, action := range actions {
		if !action.keep {
			if err := action.generation.DeleteWithExecutor(executor); err != nil {
				return err
			}
		}
	}

	//
	// Collect garbage
	//
	fmt.Fprintln(os.Stderr)
	printSection("Collecting garbage from nix store")

	if executor != nil && !executor.IsLocal() {
		// Run garbage collection on remote
		// Note: Home Manager GC doesn't require sudo (user-level operation)
		gcCmd, err := executor.Command("nix", "store", "gc", "-v")
		if err != nil {
			return err
		}
		gcCmd.SetStdout(os.Stderr)
		gcCmd.SetStderr(os.Stderr)
		return gcCmd.Run()
	} else {
		// Run garbage collection locally
		gc := osexec.CommandContext(ctx, "nix", "store", "gc", "-v")
		gc.Stdout = os.Stderr
		gc.Stderr = os.Stderr
		return gc.Run()
	}
}
