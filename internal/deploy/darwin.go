package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"runtime"
	"strings"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/generation"
)

type DarwinSystem struct{}

// ResolveName defaults to the host's LocalHostName, matching darwin-rebuild.
// nix-darwin sets LocalHostName from networking.hostName, so after the first
// activation this matches the generated system name. Outside macOS there is
// no meaningful default (the local hostname is never a darwin system), so an
// explicit name is required.
func (DarwinSystem) ResolveName(name string, _ string) (string, error) {
	if name != "" {
		return name, nil
	}
	return resolveLocalHostName(runtime.GOOS, func() ([]byte, error) {
		return osexec.Command("scutil", "--get", "LocalHostName").Output()
	})
}

func resolveLocalHostName(goos string, scutil func() ([]byte, error)) (string, error) {
	if goos != "darwin" {
		return "", errors.New("a nix-darwin system name is required when not running on macOS")
	}
	out, err := scutil()
	if err != nil {
		return "", fmt.Errorf("failed to read LocalHostName with scutil, pass a system name instead: %w", err)
	}
	n := strings.TrimSpace(string(out))
	if n == "" {
		return "", errors.New("LocalHostName is not set, pass a system name instead")
	}
	return n, nil
}

func (DarwinSystem) AttrPath(name string) string {
	return fmt.Sprintf("systems.darwin.\"%s\".result.config.system.build.toplevel", name)
}

func (DarwinSystem) CurrentGeneration(executor exec.Executor, _ string) (*Generation, error) {
	return &Generation{
		Path:    currentProfile,
		Querier: diff.NewExecutorQuerier(executor),
	}, nil
}

func (DarwinSystem) Activate(ctx context.Context, target exec.Executor, outPath string, cmd Command) error {
	if cmd != Switch {
		return nil
	}

	fmt.Fprintln(os.Stderr)
	printSection("Activating configuration")

	args := darwinActivateArgs(outPath)
	c, err := target.Command(args[0], args[1:]...)
	if err != nil {
		return err
	}
	c.SetStdin(os.Stdin)
	c.SetStderr(os.Stderr)
	c.SetStdout(os.Stdout)
	return c.Run()
}

// darwinActivateArgs mirrors `darwin-rebuild switch`: point the system profile
// at the new toplevel and run its activate script as root. Both run in a
// single privileged shell so that only one sudo password prompt is needed.
func darwinActivateArgs(outPath string) []string {
	script := fmt.Sprintf(
		"%s; nix-env -p %q --set %q && exec %q",
		generation.DarwinPathExport, systemProfile, outPath, outPath+"/activate",
	)
	return []string{"sudo", "/bin/sh", "-c", script}
}
