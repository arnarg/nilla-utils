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
	switch cmd {
	case Test:
		fmt.Fprintln(os.Stderr)
		printSection("Activating configuration")
		return runSwitchToConfig(target, outPath, "test")
	case Boot:
		fmt.Fprintln(os.Stderr)
		printSection("Adding configuration to bootloader")
		return runSwitchToConfig(target, outPath, "boot")
	case Switch:
		fmt.Fprintln(os.Stderr)
		printSection("Activating configuration")
		return runSwitchToConfig(target, outPath, "switch")
	}
	return nil
}

// switch-to-configuration exits with 4 when the configuration was activated
// but some systemd units failed to restart. That is a warning, not a failure,
// so it is tolerated and reported. Any other error, e.g. a rejected sudo
// password, is a failure.
const switchToConfigUnitFailures = 4

// runSwitchToConfig runs <outPath>/bin/switch-to-configuration <action>.
//
// For boot and switch, the system profile must point to the new toplevel
// first, since the bootloader installers enumerate generations from the
// profile. Profile update and activation are batched into one privileged
// shell so that hosts without a cached sudo credential only prompt for a
// password once. This mirrors what nixos-rebuild does.
func runSwitchToConfig(target exec.Executor, outPath, action string) error {
	args := switchToConfigArgs(outPath, action)
	c, err := target.Command(args[0], args[1:]...)
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

func switchToConfigArgs(outPath, action string) []string {
	switchp := fmt.Sprintf("%s/bin/switch-to-configuration", outPath)
	if action == "test" {
		return []string{"sudo", switchp, action}
	}
	script := fmt.Sprintf(
		"nix-env -p %q --set %q && exec %q %s",
		systemProfile, outPath, switchp, action,
	)
	return []string{"sudo", "/bin/sh", "-c", script}
}
