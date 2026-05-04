package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/util"
)

type HomeSystem struct{}

func (HomeSystem) ResolveName(name string, projectPath string) (string, error) {
	if name != "" {
		return name, nil
	}
	names, err := inferNames()
	if err != nil {
		return "", err
	}
	return findHomeConfiguration(projectPath, names)
}

func (HomeSystem) AttrPath(name string) string {
	return fmt.Sprintf("systems.home.\"%s\".result.config.home.activationPackage", name)
}

func (HomeSystem) CurrentGeneration(executor exec.Executor, name string) (*Generation, error) {
	username := extractUsername(name)
	if executor.IsLocal() {
		current, err := generation.CurrentHomeGeneration()
		if err != nil {
			return nil, err
		}
		return &Generation{
			Path:    current.Path(),
			Querier: diff.NewExecutorQuerier(executor),
		}, nil
	}
	path, found := generation.CurrentHomeGenerationPath(executor, username)
	if !found {
		return nil, fmt.Errorf("current Home Manager generation not found on target for user %s", username)
	}
	return &Generation{
		Path:    path,
		Querier: diff.NewExecutorQuerier(executor),
	}, nil
}

func (HomeSystem) Activate(ctx context.Context, executor exec.Executor, outPath string, cmd Command) error {
	if cmd != Switch {
		return nil
	}

	fmt.Fprintln(os.Stderr)
	printSection("Activating configuration")

	activatePath := fmt.Sprintf("%s/activate", outPath)
	c, err := executor.Command(activatePath)
	if err != nil {
		return fmt.Errorf("failed to create activation command: %w", err)
	}
	c.SetStdin(os.Stdin)
	c.SetStderr(os.Stderr)
	c.SetStdout(os.Stdout)
	return c.Run()
}

func inferNames() ([]string, error) {
	names := []string{}

	user := util.GetUser()
	if user == "" {
		return nil, fmt.Errorf("no user found")
	}

	if hn, err := os.Hostname(); err == nil {
		names = append(names, fmt.Sprintf("%s@%s", user, hn))
	}

	return append(names, user), nil
}

func findHomeConfiguration(p string, names []string) (string, error) {
	for _, name := range names {
		code := fmt.Sprintf("x: x ? \"%s\"", name)
		out, err := exec.NewLocalExecutor().CommandContext(
			context.Background(),
			"nix", "eval", "-f", p, "systems.home", "--apply", code,
			"--extra-experimental-features", "nix-command",
		)
		if err != nil {
			continue
		}
		buf := &bytes.Buffer{}
		out.SetStdout(buf)
		out.SetStderr(bytes.NewBuffer(nil))
		if err := out.Run(); err != nil {
			continue
		}
		if strings.TrimSpace(buf.String()) == "true" {
			return name, nil
		}
	}
	return "", fmt.Errorf("Home configurations \"%s\" not found", strings.Join(names, ", "))
}

func extractUsername(name string) string {
	if parts := strings.SplitN(name, "@", 2); len(parts) == 2 {
		return parts[0]
	}
	return name
}
