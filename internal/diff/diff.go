package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/lipgloss"
	"github.com/s0rg/set"
)

type Package struct {
	pname   string
	version string
	path    string
}

func ParsePackageFromPath(path string) *Package {
	// Parse nix store path
	res := regexp.
		MustCompile(`^/nix/store/[a-z0-9]+-(.+?)(?:-([0-9].*?))?(?:\.drv)?$`).
		FindStringSubmatch(path)

	// Check if it could be parsed
	if len(res) < 3 {
		return nil
	}

	return &Package{
		pname:   res[1],
		version: res[2],
		path:    path,
	}
}

type PackageSet struct {
	pnames   set.Unordered[string]
	packages map[string]set.Unordered[string]
}

func NewPackageSet(paths []string) PackageSet {
	pnames := make(set.Unordered[string])
	packages := map[string]set.Unordered[string]{}

	for _, p := range paths {
		pkg := ParsePackageFromPath(p)

		// Check if it could be parsed
		if pkg == nil {
			continue
		}

		// Check if there's already a package in the map
		if _, ok := packages[pkg.pname]; !ok {
			packages[pkg.pname] = make(set.Unordered[string])
		}

		// Add package to map
		lst := packages[pkg.pname]
		lst.Add(pkg.version)

		// Add package to pnames
		pnames.Add(pkg.pname)
	}

	return PackageSet{
		pnames:   pnames,
		packages: packages,
	}
}

func (s *PackageSet) HasPackage(pname string) bool {
	if pkgs, ok := s.packages[pname]; ok && len(pkgs) > 0 {
		return true
	}
	return false
}

func (s *PackageSet) GetPackagesVersions(pname string) set.Unordered[string] {
	if !s.HasPackage(pname) {
		return nil
	}

	return s.packages[pname]
}

func (s *PackageSet) NumPackages() int {
	return len(s.packages)
}

type PackageDiff struct {
	PName  string
	Before []string
	After  []string
}

func sortPackageDiff(a, b PackageDiff) int {
	return strings.Compare(strings.ToLower(a.PName), strings.ToLower(b.PName))
}

type Diff struct {
	Changed []PackageDiff
	Added   []PackageDiff
	Removed []PackageDiff
}

func Calculate(from, to PackageSet) Diff {
	changed := []PackageDiff{}
	added := []PackageDiff{}
	removed := []PackageDiff{}

	// Use the pname sets to find what differs
	remainingPNames := set.Intersect(from.pnames, to.pnames)
	addedPNames := set.Diff(to.pnames, from.pnames)
	removedPNames := set.Diff(from.pnames, to.pnames)

	// Find actual differing versions between remaining
	// packages
	for pname := range remainingPNames.Iter {
		fromVersions := from.GetPackagesVersions(pname)
		toVersions := to.GetPackagesVersions(pname)

		if !set.Equal(fromVersions, toVersions) {
			// Get slices with version before and after
			before := set.ToSlice(fromVersions)
			after := set.ToSlice(toVersions)

			// Sort slices
			slices.Sort(before)
			slices.Sort(after)

			// Append diff to list
			changed = append(
				changed,
				PackageDiff{
					PName:  pname,
					Before: before,
					After:  after,
				},
			)
		}
	}
	slices.SortFunc(changed, sortPackageDiff)

	// Go through added pnames and get the versions
	// to make a package diff
	for pname := range addedPNames.Iter {
		// Create a slice of added package versions
		after := set.ToSlice(to.GetPackagesVersions(pname))
		slices.Sort(after)

		added = append(
			added,
			PackageDiff{
				PName:  pname,
				Before: []string{},
				After:  after,
			},
		)
	}
	slices.SortFunc(added, sortPackageDiff)

	// Go through remove pnames and get the versions
	// to make a package diff
	for pname := range removedPNames.Iter {
		// Creata a slice of removed package versions
		before := set.ToSlice(from.GetPackagesVersions(pname))
		slices.Sort(before)

		removed = append(
			removed,
			PackageDiff{
				PName:  pname,
				Before: before,
				After:  []string{},
			},
		)
	}
	slices.SortFunc(removed, sortPackageDiff)

	return Diff{changed, added, removed}
}

type Generation struct {
	Path     string
	Executor exec.Executor
}

type ClosureDiff struct {
	NumBefore   int
	NumAfter    int
	BytesBefore int64
	BytesAfter  int64
}

func Run(from, to *Generation) (*Diff, *ClosureDiff, error) {
	// Query before
	beforePaths, err := queryGeneration(from.Path, from.Executor)
	if err != nil {
		return nil, nil, err
	}

	// Query after
	afterPaths, err := queryGeneration(to.Path, to.Executor)
	if err != nil {
		return nil, nil, err
	}

	// Parse paths
	before := NewPackageSet(beforePaths)
	after := NewPackageSet(afterPaths)

	// Calculate diff
	diff := Calculate(before, after)

	// Create closure diff
	closure := &ClosureDiff{
		NumBefore: before.NumPackages(),
		NumAfter:  after.NumPackages(),
	}

	// Get closure size from before
	closure.BytesBefore, err = getClosureSize(from.Executor, from.Path)
	if err != nil {
		return nil, nil, err
	}

	// Get closure size from after
	closure.BytesAfter, err = getClosureSize(to.Executor, to.Path)
	if err != nil {
		return nil, nil, err
	}

	return &diff, closure, nil
}

func queryGeneration(path string, executor exec.Executor) ([]string, error) {
	paths := []string{}

	swPath := path + "/sw"

	// Check if /sw exists
	swExists, err := executor.PathExists(swPath)
	if err != nil {
		return nil, err
	}
	if !swExists {
		swPath = path
	}

	// Query references
	refs, err := runNixStoreOutput(executor, "--query", "--references", swPath)
	if err != nil {
		return nil, err
	}

	paths = append(paths, strings.Split(string(refs), "\n")...)

	// Query requisites
	reqs, err := runNixStoreOutput(executor, "--query", "--requisites", path)
	if err != nil {
		return nil, err
	}

	paths = append(paths, strings.Split(string(reqs), "\n")...)

	return paths, nil
}

func runNixStoreOutput(executor exec.Executor, args ...string) ([]byte, error) {
	// Create buffer for output
	buf := &bytes.Buffer{}

	// Create command from executor
	cmd, err := executor.Command("nix-store", args...)
	if err != nil {
		return nil, err
	}
	cmd.SetStdout(buf)

	// Run command
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func getClosureSize(executor exec.Executor, path string) (int64, error) {
	// Create buffer for output
	buf := &bytes.Buffer{}

	// Create command from executor
	cmd, err := executor.Command("nix", "path-info", "--json", "--closure-size", path)
	if err != nil {
		return 0, err
	}
	cmd.SetStdout(buf)

	// Run command
	if err := cmd.Run(); err != nil {
		return 0, err
	}

	// Parse path-info output
	info := []struct {
		ClosureSize int64 `json:"closureSize"`
	}{}
	if err := json.Unmarshal(buf.Bytes(), &info); err != nil {
		return 0, err
	}

	if len(info) < 1 {
		return 0, nil
	}

	return info[0].ClosureSize, nil
}

func Print(diff *Diff) {
	if len(diff.Changed) > 0 {
		fmt.Println("Version changes:")
		widest := 0
		for _, pkg := range diff.Changed {
			if len(pkg.PName) > widest {
				widest = len(pkg.PName)
			}
		}

		for i, pkg := range diff.Changed {
			fmt.Fprintf(
				os.Stderr,
				"#%02d  %s  %s -> %s\n",
				i+1,
				lipgloss.NewStyle().
					Width(widest).
					Foreground(lipgloss.Color("10")).
					SetString(pkg.PName).
					String(),
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("3")).
					SetString(strings.Join(pkg.Before, ", ")).
					String(),
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("3")).
					SetString(strings.Join(pkg.After, ", ")).
					String(),
			)
		}
	}

	if len(diff.Added) > 0 {
		fmt.Println("Added packages:")
		widest := 0
		for _, pkg := range diff.Added {
			if len(pkg.PName) > widest {
				widest = len(pkg.PName)
			}
		}

		for i, pkg := range diff.Added {
			fmt.Fprintf(
				os.Stderr,
				"#%02d  %s  %s\n",
				i+1,
				lipgloss.NewStyle().
					Width(widest).
					Foreground(lipgloss.Color("10")).
					SetString(pkg.PName).
					String(),
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("3")).
					SetString(strings.Join(pkg.After, ", ")).
					String(),
			)
		}
	}

	if len(diff.Removed) > 0 {
		fmt.Println("Removed packages:")
		widest := 0
		for _, pkg := range diff.Removed {
			if len(pkg.PName) > widest {
				widest = len(pkg.PName)
			}
		}

		for i, pkg := range diff.Removed {
			fmt.Fprintf(
				os.Stderr,
				"#%02d  %s  %s\n",
				i+1,
				lipgloss.NewStyle().
					Width(widest).
					Foreground(lipgloss.Color("10")).
					SetString(pkg.PName).
					String(),
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("3")).
					SetString(strings.Join(pkg.Before, ", ")).
					String(),
			)
		}
	}
}

func Execute(from, to *Generation) error {
	// Just execute nvd diff if both are local, for now
	if from.Executor.IsLocal() && to.Executor.IsLocal() {
		diff, _ := to.Executor.Command("nvd", "diff", from.Path, to.Path)
		diff.SetStderr(os.Stderr)
		diff.SetStdout(os.Stderr)
		if err := diff.Run(); err != nil {
			return err
		}

		return nil
	}

	// Otherwise we calculate it locally and print
	pkgDiff, closure, err := Run(from, to)
	if err != nil {
		return err
	}

	Print(pkgDiff)

	sizeDiff, negative, unit := util.DiffBytes(closure.BytesBefore, closure.BytesAfter)

	prefix := "+"
	if negative {
		prefix = "-"
	}
	diskUsage := fmt.Sprintf("%s%.2f%s", prefix, sizeDiff, unit)

	fmt.Fprintf(
		os.Stderr,
		"Closure size: %d -> %d (disk usage %s)\n",
		closure.NumBefore,
		closure.NumAfter,
		diskUsage,
	)

	return nil
}
