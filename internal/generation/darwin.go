package generation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
)

// darwinPath makes nix-env findable under sudo and in non-interactive SSH
// sessions, where the Nix profile is often missing from PATH on macOS.
const darwinPath = `export PATH="$PATH:/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin"`

// DarwinSystem implements System for nix-darwin generations. They use the same
// profile layout as NixOS (/nix/var/nix/profiles/system-N-link), so deletion
// and garbage collection are shared with NixOSSystem.
type DarwinSystem struct{}

func (DarwinSystem) RequiresLocalRoot() bool { return true }

func (DarwinSystem) Headers() []string {
	return []string{"Generation", "Build date", "nix-darwin version"}
}

func (DarwinSystem) Row(g Generation) []string {
	return []string{
		strconv.Itoa(g.ID),
		g.BuildDate.Format(time.DateTime),
		g.Version,
	}
}

func (DarwinSystem) Current(h exec.Host) (Generation, error) {
	res, err := h.Readlink(nixosCurrentLink)
	if err != nil {
		return Generation{}, err
	}
	ei, err := h.Lstat(filepath.Join(nixosProfilesDir, res))
	if err != nil {
		return Generation{}, err
	}
	return buildDarwinGeneration(h, nixosProfilesDir, ei)
}

func (DarwinSystem) List(h exec.Host) ([]Generation, error) {
	entries, err := h.ReadDir(nixosProfilesDir)
	if err != nil {
		return nil, err
	}

	var gens []Generation
	for _, e := range entries {
		if !e.IsSymlink || !nixosGenListRe.MatchString(e.Name) {
			continue
		}
		g, err := buildDarwinGeneration(h, nixosProfilesDir, e)
		if err != nil {
			return nil, err
		}
		gens = append(gens, g)
	}
	return gens, nil
}

func (DarwinSystem) DeleteGenerations(h exec.Host, gens []Generation) error {
	return NixOSSystem{}.DeleteGenerations(h, gens)
}

func (DarwinSystem) CollectGarbage(ctx context.Context, h exec.Host) error {
	return NixOSSystem{}.CollectGarbage(ctx, h)
}

// Rollback mirrors `darwin-rebuild --rollback`: switch the system profile to
// the chosen generation and run its activate script as root, in one shell.
func (DarwinSystem) Rollback(ctx context.Context, h exec.Host, gen Generation) error {
	args := darwinRollbackArgs(gen)
	if !h.IsLocal() {
		args = append([]string{"sudo"}, args...)
	}
	return runCmd(ctx, h, args[0], args[1:]...)
}

func darwinRollbackArgs(gen Generation) []string {
	script := fmt.Sprintf(
		"%s; nix-env -p %q --switch-generation %d && exec %q",
		darwinPath, nixosCurrentLink, gen.ID, filepath.Join(gen.path, "activate"),
	)
	return []string{"/bin/sh", "-c", script}
}

func buildDarwinGeneration(h exec.Host, root string, e exec.EntryInfo) (Generation, error) {
	m := nixosGenIDRe.FindStringSubmatch(e.Name)
	if m == nil {
		return Generation{}, fmt.Errorf("generation path '%s' does not match pattern", e.Name)
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		return Generation{}, err
	}

	path := filepath.Join(root, e.Name)

	version := "Unknown"
	verBytes, err := h.ReadFile(filepath.Join(path, "darwin-version"))
	if err == nil {
		version = strings.TrimSpace(string(verBytes))
	} else if !errors.Is(err, os.ErrNotExist) {
		return Generation{}, err
	}

	return Generation{
		ID:        id,
		BuildDate: e.ModTime,
		Version:   version,
		path:      path,
	}, nil
}
