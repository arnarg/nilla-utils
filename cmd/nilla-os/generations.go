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
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/lipgloss"
	"github.com/urfave/cli/v3"
)

func sortGenerationsDesc(generations []*generation.NixOSGeneration) {
	// Sort the list of generations by ID descending
	slices.SortFunc(generations, func(a, b *generation.NixOSGeneration) int {
		return cmp.Compare(b.ID, a.ID)
	})
}

func setupExecutor(targetStr string) (exec.Executor, error) {
	if targetStr == "" {
		return nil, nil
	}

	executor, err := exec.NewSSHExecutor(targetStr)
	if err != nil {
		return nil, fmt.Errorf("failed to setup SSH executor for target %s: %w", targetStr, err)
	}
	return executor, nil
}

func listGenerations(ctx context.Context, cmd *cli.Command) error {
	if err := util.ValidateArgs(cmd, 0); err != nil {
		return err
	}

	executor, err := setupExecutor(cmd.String("target"))
	if err != nil {
		return err
	}

	// Get current generation
	current, err := generation.CurrentNixOSGeneration(executor)
	if err != nil {
		return err
	}

	// List all generations
	generations, err := generation.ListNixOSGenerations(executor)
	if err != nil {
		return err
	}

	// Sort the list in reverse by ID
	sortGenerationsDesc(generations)

	// Build table
	headers := []string{"Generation", "Build date", "NixOS version", "Kernel version"}
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
			gen.KernelVersion,
		})
	}

	fmt.Println(util.RenderTable(headers, rows...))

	return nil
}

type genAction struct {
	generation *generation.NixOSGeneration
	keep       bool
}

func cleanGenerations(ctx context.Context, cmd *cli.Command) error {
	if err := util.ValidateArgs(cmd, 0); err != nil {
		return err
	}

	executor, err := setupExecutor(cmd.String("target"))
	if err != nil {
		return err
	}

	if executor == nil {
		// We need to self-elevate if we're not root before continuing (only for local)
		if !util.IsRoot() {
			return util.SelfElevate()
		}
	}

	// Parse parameters
	keep := cmd.Uint("keep")
	foundCurrent := false

	// Get current generation
	current, err := generation.CurrentNixOSGeneration(executor)
	if err != nil {
		return err
	}

	// List all generations
	generations, err := generation.ListNixOSGenerations(executor)
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
	headers := []string{"Generation", "Build date", "NixOS version", "Kernel version"}
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
			style.SetString(gen.KernelVersion).String(),
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
				return fmt.Errorf("failed to delete generation %d: %w", action.generation.ID, err)
			}
		}
	}

	//
	// Collect garbage
	//
	fmt.Fprintln(os.Stderr)
	printSection("Collecting garbage from nix store")

	if executor != nil && !executor.IsLocal() {
		// Run garbage collection on remote (requires root/sudo)
		// Note: NixOS GC requires sudo because it's a system-level operation
		gcCmd, err := exec.CommandWithSudoIfNeeded(executor, "nix", "store", "gc", "-v")
		if err != nil {
			return fmt.Errorf("failed to create garbage collection command: %w", err)
		}
		// Set stdin to enable PTY allocation for sudo password prompt
		gcCmd.SetStdin(os.Stdin)
		gcCmd.SetStdout(os.Stderr)
		gcCmd.SetStderr(os.Stderr)
		err = gcCmd.Run()
		if err != nil {
			return fmt.Errorf("garbage collection failed: %w", err)
		}
		return nil
	} else {
		// Run garbage collection locally
		gc := osexec.CommandContext(ctx, "nix", "store", "gc", "-v")
		gc.Stdout = os.Stderr
		gc.Stderr = os.Stderr
		return gc.Run()
	}
}
