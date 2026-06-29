// Package gencmd holds the shared presentation logic for listing and cleaning
// NixOS and Home Manager generations. It drives a generation.System against an
// exec.Host, keeping presentation out of the pure-logic generation package and
// out of the thin command handlers.
package gencmd

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"

	"charm.land/lipgloss/v2"
	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
)

var (
	currentMarker = lipgloss.NewStyle().
			Foreground(lipgloss.Color("13")).
			Bold(true).
			SetString("*").
			String()
	keepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

type action struct {
	gen  generation.Generation
	keep bool
}

// List prints all generations of sys (on target, or locally when target is
// empty), marking the current one.
func List(ctx context.Context, sys generation.System, target string) error {
	h, err := exec.NewHost(ctx, target, nil)
	if err != nil {
		return err
	}
	defer h.Close()

	current, err := sys.Current(h)
	if err != nil {
		return err
	}

	generations, err := sys.List(h)
	if err != nil {
		return err
	}

	sortDesc(generations)

	rows := make([][]string, 0, len(generations))
	for _, g := range generations {
		rows = append(rows, withCurrentMarker(sys.Row(g), g.ID == current.ID))
	}

	fmt.Println(util.RenderTable(sys.Headers(), rows...))
	return nil
}

// Clean builds a keep/delete plan, asks for confirmation, removes the doomed
// generation links and runs garbage collection. For systems that require local
// root privileges it self-elevates before opening the host.
func Clean(ctx context.Context, sys generation.System, target string, keep uint, confirm bool) error {
	// SelfElevate replaces the process, so it must run before NewHost.
	if target == "" && sys.RequiresLocalRoot() && !util.IsRoot() {
		return util.SelfElevate()
	}

	h, err := exec.NewHost(ctx, target, nil)
	if err != nil {
		return err
	}
	defer h.Close()

	current, err := sys.Current(h)
	if err != nil {
		return err
	}

	generations, err := sys.List(h)
	if err != nil {
		return err
	}

	sortDesc(generations)

	actions := buildPlan(generations, current, keep)

	rows := make([][]string, 0, len(actions))
	for _, a := range actions {
		rows = append(rows, planRow(sys.Row(a.gen), a, a.gen.ID == current.ID))
	}

	printSection("Plan")
	fmt.Fprintln(os.Stderr, util.RenderTable(sys.Headers(), rows...))

	if !confirm {
		ok, err := tui.RunConfirm("Do you want to continue?")
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}

	var toDelete []generation.Generation
	for _, a := range actions {
		if !a.keep {
			toDelete = append(toDelete, a.gen)
		}
	}

	if err := sys.DeleteGenerations(h, toDelete); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr)
	printSection("Collecting garbage from nix store")
	return sys.CollectGarbage(ctx, h)
}

// buildPlan mirrors the original keep/current heuristic: keep the newest `keep`
// generations, always keep the current one, and never delete the current one to
// satisfy the last remaining slot.
func buildPlan(gens []generation.Generation, current generation.Generation, keep uint) []action {
	remaining := keep
	foundCurrent := false
	actions := make([]action, 0, len(gens))
	for _, g := range gens {
		doKeep := remaining > 0

		if g.ID == current.ID {
			doKeep = true
			foundCurrent = true
		} else if !foundCurrent && remaining == 1 {
			doKeep = false
		}

		if doKeep {
			remaining -= 1
		}

		actions = append(actions, action{g, doKeep})
	}
	return actions
}

func sortDesc(gens []generation.Generation) {
	slices.SortFunc(gens, func(a, b generation.Generation) int {
		return cmp.Compare(b.ID, a.ID)
	})
}

func withCurrentMarker(row []string, current bool) []string {
	pre := " "
	if current {
		pre = currentMarker
	}
	cells := append([]string(nil), row...)
	cells[0] = fmt.Sprintf("%s %s", pre, cells[0])
	return cells
}

func planRow(row []string, a action, current bool) []string {
	pre := " "
	if current {
		pre = currentMarker
	}
	style := delStyle
	if a.keep {
		style = keepStyle
	}
	cells := make([]string, len(row))
	for i, c := range row {
		cells[i] = style.SetString(c).String()
	}
	cells[0] = fmt.Sprintf("%s %s", pre, cells[0])
	return cells
}

func printSection(text string) {
	fmt.Fprintf(os.Stderr, "\033[32m>\033[0m %s\n", text)
}
