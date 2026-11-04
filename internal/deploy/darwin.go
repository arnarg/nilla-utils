package deploy

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
)

type DarwinSystem struct{}

// ResolveName defaults to the host's LocalHostName, matching darwin-rebuild.
// When scutil is unavailable (e.g. deploying from Linux) it falls back to the
// hostname with any ".local" suffix removed.
func (DarwinSystem) ResolveName(name string, _ string) (string, error) {
	if name != "" {
		return name, nil
	}
	if out, err := osexec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
		if n := strings.TrimSpace(string(out)); n != "" {
			return n, nil
		}
	}
	h, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(h, ".local"), nil
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

// darwinActivatePath makes nix-env findable under sudo and in non-interactive
// SSH sessions, where the Nix profile is often missing from PATH on macOS.
const darwinActivatePath = `export PATH="$PATH:/run/current-system/sw/bin:/nix/var/nix/profiles/default/bin"`

// darwinActivateArgs mirrors `darwin-rebuild switch`: point the system profile
// at the new toplevel and run its activate script as root. Both run in a
// single privileged shell so that only one sudo password prompt is needed.
func darwinActivateArgs(outPath string) []string {
	script := fmt.Sprintf(
		"%s; nix-env -p %q --set %q && exec %q",
		darwinActivatePath, systemProfile, outPath, outPath+"/activate",
	)
	return []string{"sudo", "/bin/sh", "-c", script}
}
