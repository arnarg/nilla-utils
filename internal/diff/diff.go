package diff

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Generation represents a path to a generation and an executor that
// can query all nix paths in its closure.
type Generation struct {
	Path    string
	Querier StoreQuerier
}

type PackageName string
type Version string

type Package struct {
	Name    PackageName
	Version Version
	Path    string
}

type ChangeType int

const (
	Changed ChangeType = iota
	Added
	Removed
)

type Change struct {
	Name   PackageName
	Before []Version
	After  []Version
	Type   ChangeType
}

type Report struct {
	Changes     []Change
	NumBefore   int
	NumAfter    int
	BytesBefore int64
	BytesAfter  int64
}

// CalculateReport computes the diff between two generations.
func CalculateReport(ctx context.Context, from, to *Generation) (Report, error) {
	// Query package lists
	before, err := from.Querier.QueryPackages(ctx, from.Path)
	if err != nil {
		return Report{}, fmt.Errorf("failed to query from generation: %w", err)
	}

	after, err := to.Querier.QueryPackages(ctx, to.Path)
	if err != nil {
		return Report{}, fmt.Errorf("failed to query to generation: %w", err)
	}

	// Query closure sizes
	beforeSize, err := from.Querier.GetClosureSize(ctx, from.Path)
	if err != nil {
		return Report{}, fmt.Errorf("failed to get closure size for from: %w", err)
	}

	afterSize, err := to.Querier.GetClosureSize(ctx, to.Path)
	if err != nil {
		return Report{}, fmt.Errorf("failed to get closure size for to: %w", err)
	}

	// Calculate pure diff
	report := calculatePackageDiff(before, after)
	report.BytesBefore = beforeSize
	report.BytesAfter = afterSize

	return report, nil
}

func calculatePackageDiff(before, after []Package) Report {
	// Index by name -> set of versions
	beforeIdx := make(map[PackageName]map[Version]struct{})
	afterIdx := make(map[PackageName]map[Version]struct{})

	for _, p := range before {
		if _, ok := beforeIdx[p.Name]; !ok {
			beforeIdx[p.Name] = make(map[Version]struct{})
		}
		beforeIdx[p.Name][p.Version] = struct{}{}
	}
	for _, p := range after {
		if _, ok := afterIdx[p.Name]; !ok {
			afterIdx[p.Name] = make(map[Version]struct{})
		}
		afterIdx[p.Name][p.Version] = struct{}{}
	}

	var changes []Change

	// Check for changes and removals
	for name, vers := range beforeIdx {
		if other, exists := afterIdx[name]; !exists {
			changes = append(changes, Change{
				Name:   name,
				Before: setToSlice(vers),
				After:  nil,
				Type:   Removed,
			})
		} else if !slices.Equal(setToSlice(vers), setToSlice(other)) {
			changes = append(changes, Change{
				Name:   name,
				Before: setToSlice(vers),
				After:  setToSlice(other),
				Type:   Changed,
			})
		}
	}

	// Check for additions
	for name, vers := range afterIdx {
		if _, exists := beforeIdx[name]; !exists {
			changes = append(changes, Change{
				Name:   name,
				Before: nil,
				After:  setToSlice(vers),
				Type:   Added,
			})
		}
	}

	// Sort case-insensitively by name
	slices.SortFunc(changes, func(a, b Change) int {
		return cmp.Compare(strings.ToLower(string(a.Name)), strings.ToLower(string(b.Name)))
	})

	return Report{
		Changes:   changes,
		NumBefore: len(beforeIdx),
		NumAfter:  len(afterIdx),
	}
}

func setToSlice(m map[Version]struct{}) []Version {
	s := make([]Version, 0, len(m))
	for v := range m {
		s = append(s, v)
	}
	slices.Sort(s)
	return s
}

// Execute a diff between two generations and use the default terminal
// renderer to print the output to terminal output.
func Execute(from, to *Generation) error {
	// Calculate diff
	report, err := CalculateReport(context.Background(), from, to)
	if err != nil {
		return err
	}

	// Create a terminal renderer
	renderer := NewTerminalRenderer()

	// Render report
	if err := renderer.Render(os.Stderr, report); err != nil {
		return fmt.Errorf("render failed: %w", err)
	}

	return nil
}
