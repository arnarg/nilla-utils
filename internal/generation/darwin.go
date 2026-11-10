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

// DarwinPathExport makes Nix binaries findable under sudo and in
// non-interactive SSH sessions, where the Nix profile is often missing from
// PATH on macOS. Prefix any shell script that calls nix or nix-env with it.
const DarwinPathExport = `export PATH="$PATH:/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin"`

// DarwinSystem implements System for nix-darwin generations. They use the same
// profile layout as NixOS (/nix/var/nix/profiles/system-N-link), so listing
// and deletion are shared with NixOSSystem.
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
	return currentSystemGeneration(h, buildDarwinGeneration)
}

func (DarwinSystem) List(h exec.Host) ([]Generation, error) {
	return listSystemGenerations(h, buildDarwinGeneration)
}

func (DarwinSystem) DeleteGenerations(h exec.Host, gens []Generation) error {
	return NixOSSystem{}.DeleteGenerations(h, gens)
}

// CollectGarbage runs nix store gc through a shell with DarwinPathExport, as
// nix is often not on PATH under sudo on macOS.
func (DarwinSystem) CollectGarbage(ctx context.Context, h exec.Host) error {
	args := darwinShellArgs(h, fmt.Sprintf("%s; exec %s", DarwinPathExport, strings.Join(nixGCArgs, " ")))
	return runGC(ctx, h, args[0], args[1:]...)
}

// Rollback mirrors `darwin-rebuild --rollback`: switch the system profile to
// the chosen generation and run its activate script as root, in one shell.
func (DarwinSystem) Rollback(ctx context.Context, h exec.Host, gen Generation) error {
	script := fmt.Sprintf(
		"%s; nix-env -p %q --switch-generation %d && exec %q",
		DarwinPathExport, nixosCurrentLink, gen.ID, filepath.Join(gen.path, "activate"),
	)
	args := darwinShellArgs(h, script)
	return runCmd(ctx, h, args[0], args[1:]...)
}

// darwinShellArgs wraps script in /bin/sh -c, under sudo on remote hosts.
// Local commands already run as root after SelfElevate.
func darwinShellArgs(h exec.Host, script string) []string {
	args := []string{"/bin/sh", "-c", script}
	if !h.IsLocal() {
		args = append([]string{"sudo"}, args...)
	}
	return args
}

func buildDarwinGeneration(h exec.Host, root string, e exec.EntryInfo) (Generation, error) {
	id, path, err := parseSystemGenLink(root, e)
	if err != nil {
		return Generation{}, err
	}

	// nix-darwin writes darwin-version into every generation; older or
	// hand-built profiles may lack it.
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
