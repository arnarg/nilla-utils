package generation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
)

const nixosProfilesDir = "/nix/var/nix/profiles"

var (
	nixosCurrentLink = nixosProfilesDir + "/system"
	nixosGenIDRe     = regexp.MustCompile(`^system-(\d+)-link$`)
	nixosGenListRe   = regexp.MustCompile(`^system-\d+-link$`)
	kernelVersionRe  = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// NixOSSystem implements System for NixOS generations stored under
// /nix/var/nix/profiles.
type NixOSSystem struct{}

func (NixOSSystem) RequiresLocalRoot() bool { return true }

func (NixOSSystem) Headers() []string {
	return []string{"Generation", "Build date", "NixOS version", "Kernel version"}
}

func (NixOSSystem) Row(g Generation) []string {
	return []string{
		strconv.Itoa(g.ID),
		g.BuildDate.Format(time.DateTime),
		g.Version,
		g.KernelVersion,
	}
}

func (NixOSSystem) Current(h exec.Host) (Generation, error) {
	// The main system profile (/nix/var/nix/profiles/system) is a symlink to
	// the current generation link (e.g. system-5-link).
	res, err := h.Readlink(nixosCurrentLink)
	if err != nil {
		return Generation{}, err
	}
	absp := filepath.Join(nixosProfilesDir, res)
	ei, err := h.Lstat(absp)
	if err != nil {
		return Generation{}, err
	}
	return buildNixOSGeneration(h, nixosProfilesDir, ei)
}

func (NixOSSystem) List(h exec.Host) ([]Generation, error) {
	entries, err := h.ReadDir(nixosProfilesDir)
	if err != nil {
		return nil, err
	}

	var gens []Generation
	for _, e := range entries {
		if !e.IsSymlink || !nixosGenListRe.MatchString(e.Name) {
			continue
		}
		g, err := buildNixOSGeneration(h, nixosProfilesDir, e)
		if err != nil {
			return nil, err
		}
		gens = append(gens, g)
	}
	return gens, nil
}

func (NixOSSystem) DeleteGenerations(h exec.Host, gens []Generation) error {
	if len(gens) == 0 {
		return nil
	}
	// Remote NixOS profile links are root-owned: batch into a single privileged
	// removal to avoid one password prompt per link. Local cleanup runs after
	// SelfElevate, so the process is already root.
	if !h.IsLocal() {
		args := []string{"rm"}
		for _, g := range gens {
			args = append(args, g.path)
		}
		c, err := h.Command("sudo", args...)
		if err != nil {
			return err
		}
		c.SetStdin(os.Stdin)
		c.SetStderr(os.Stderr)
		c.SetStdout(os.Stdout)
		return c.Run()
	}
	for _, g := range gens {
		if err := h.Remove(g.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (NixOSSystem) Rollback(ctx context.Context, h exec.Host, gen Generation) error {
	if h.IsLocal() {
		if err := runCmd(ctx, h, "nix-env", "-p", nixosCurrentLink,
			"--switch-generation", strconv.Itoa(gen.ID)); err != nil {
			return err
		}
		switchp := filepath.Join(gen.path, "bin", "switch-to-configuration")
		return runCmd(ctx, h, switchp, "switch")
	}
	if err := runCmd(ctx, h, "sudo", "nix-env", "-p", nixosCurrentLink,
		"--switch-generation", strconv.Itoa(gen.ID)); err != nil {
		return err
	}
	switchp := filepath.Join(gen.path, "bin", "switch-to-configuration")
	return runCmd(ctx, h, "sudo", switchp, "switch")
}

func (NixOSSystem) CollectGarbage(ctx context.Context, h exec.Host) error {
	// Local cleanup already elevated to root; remote gc must run under sudo.
	if h.IsLocal() {
		return runGC(ctx, h, "nix", "store", "gc", "-v")
	}
	return runGC(ctx, h, "sudo", "nix", "store", "gc", "-v")
}

func buildNixOSGeneration(h exec.Host, root string, e exec.EntryInfo) (Generation, error) {
	m := nixosGenIDRe.FindStringSubmatch(e.Name)
	if m == nil {
		return Generation{}, fmt.Errorf("generation path '%s' does not match pattern", e.Name)
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		return Generation{}, err
	}

	path := filepath.Join(root, e.Name)

	verBytes, err := h.ReadFile(filepath.Join(path, "nixos-version"))
	if err != nil {
		return Generation{}, err
	}

	kernel, err := readKernelVersion(h, path)
	if err != nil {
		return Generation{}, err
	}

	return Generation{
		ID:            id,
		BuildDate:     e.ModTime,
		Version:       strings.TrimSpace(string(verBytes)),
		KernelVersion: kernel,
		path:          path,
	}, nil
}

func readKernelVersion(h exec.Host, system string) (string, error) {
	entries, err := h.ReadDir(filepath.Join(system, "kernel-modules", "lib", "modules"))
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if kernelVersionRe.MatchString(e.Name) {
			return e.Name, nil
		}
	}
	return "Unknown", nil
}

func runGC(ctx context.Context, h exec.Host, name string, args ...string) error {
	c, err := h.CommandContext(ctx, name, args...)
	if err != nil {
		return err
	}
	c.SetStdin(os.Stdin)
	c.SetStderr(os.Stderr)
	c.SetStdout(os.Stderr)
	return c.Run()
}

func runCmd(ctx context.Context, h exec.Host, name string, args ...string) error {
	c, err := h.CommandContext(ctx, name, args...)
	if err != nil {
		return err
	}
	c.SetStdin(os.Stdin)
	c.SetStderr(os.Stderr)
	c.SetStdout(os.Stdout)
	return c.Run()
}
