package deploy

import (
	"context"
	"fmt"
	"os"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
)

const (
	systemProfile  = "/nix/var/nix/profiles/system"
	currentProfile = "/run/current-system"
)

type NixOSSystem struct{}

func (NixOSSystem) ResolveName(name string, _ string) (string, error) {
	if name != "" {
		return name, nil
	}
	return os.Hostname()
}

func (NixOSSystem) AttrPath(name string) string {
	return fmt.Sprintf("systems.nixos.\"%s\".result.config.system.build.toplevel", name)
}

func (NixOSSystem) CurrentGeneration(executor exec.Executor, _ string) (*Generation, error) {
	return &Generation{
		Path:    currentProfile,
		Querier: diff.NewExecutorQuerier(executor),
	}, nil
}

func (NixOSSystem) Activate(ctx context.Context, target exec.Executor, outPath string, cmd Command) error {
	if cmd == Test || cmd == Switch {
		fmt.Fprintln(os.Stderr)
		printSection("Activating configuration")
		if err := runSwitchToConfig(target, outPath, "test"); err != nil {
			return err
		}
	}

	if cmd == Boot || cmd == Switch {
		fmt.Fprintln(os.Stderr)
		printSection("Adding configuration to bootloader")
		if err := setProfile(target, outPath); err != nil {
			return err
		}
		return runSwitchToConfig(target, outPath, "boot")
	}

	return nil
}

// switch-to-configuration exits with 4 when the configuration was activated
// but some systemd units failed to restart. That is a warning, not a failure,
// so it is tolerated and reported. Any other error, e.g. a rejected sudo
// password, is a failure.
const switchToConfigUnitFailures = 4

func runSwitchToConfig(target exec.Executor, outPath string, action string) error {
	switchp := fmt.Sprintf("%s/bin/switch-to-configuration", outPath)
	c, err := target.Command("sudo", switchp, action)
	if err != nil {
		return err
	}
	c.SetStdin(os.Stdin)
	c.SetStderr(os.Stderr)
	c.SetStdout(os.Stdout)
	if err := c.Run(); err != nil {
		if exec.ExitCode(err) == switchToConfigUnitFailures {
			log.Warnf("Some systemd units failed to restart, check the output above for details")
			return nil
		}
		return err
	}
	return nil
}

func setProfile(target exec.Executor, outPath string) error {
	c, err := target.Command(
		"sudo", "nix", "build",
		"--no-link", "--profile", systemProfile,
		"--extra-experimental-features", "nix-command",
		outPath,
	)
	if err != nil {
		return err
	}
	c.SetStdin(os.Stdin)
	c.SetStderr(os.Stderr)
	c.SetStdout(os.Stdout)
	return c.Run()
}
