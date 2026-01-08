package nix

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/log"
	"github.com/sourcegraph/conc/pool"
)

type NixCommand struct {
	cmd   string
	args  []string
	exec  exec.Executor
	stdin io.Reader

	privileged bool

	reporter ProgressReporter
}

func Command(cmd string) NixCommand {
	return NixCommand{
		cmd:  cmd,
		exec: exec.NewLocalExecutor(),
	}
}

func (c NixCommand) Args(args []string) NixCommand {
	c.args = args
	return c
}

func (c NixCommand) Executor(executor exec.Executor) NixCommand {
	c.exec = executor
	return c
}

func (c NixCommand) Stdin(r io.Reader) NixCommand {
	c.stdin = r
	return c
}

func (c NixCommand) Privileged(privileged bool) NixCommand {
	c.privileged = privileged
	return c
}

func (c NixCommand) Reporter(reporter ProgressReporter) NixCommand {
	c.reporter = reporter
	return c
}

func (c NixCommand) Run(ctx context.Context) ([]byte, error) {
	cmd := "nix"
	args := []string{}

	// Check if we need to run with sudo
	if c.privileged {
		cmd = "sudo"
		args = append(args, "nix")
	}

	// Append rest of arguments
	args = append(args, c.cmd, "--extra-experimental-features", "nix-command")
	args = append(args, c.args...)
	if c.cmd == "build" {
		args = append(args, "--print-out-paths")
	}

	// Debug: log the full command being executed
	log.Debugf("Executing: %s %s", cmd, fmt.Sprintf("%v", args))

	if c.reporter != nil {
		return c.runWithReporter(ctx, cmd, args)
	}

	return c.runStdout(ctx, cmd, args)
}

func (c NixCommand) runStdout(ctx context.Context, cmd string, args []string) ([]byte, error) {
	// Create nix command
	nixc, err := c.exec.CommandContext(ctx, cmd, args...)
	if err != nil {
		return nil, err
	}

	// Create a buffer to capture nix's stdout
	b := &bytes.Buffer{}

	// Plug stdout and stderr
	nixc.SetStdout(b)
	nixc.SetStderr(os.Stderr)

	// Plug stdin if provided
	if c.stdin != nil {
		nixc.SetStdin(c.stdin)
	}

	// Run nix command
	if err := nixc.Run(); err != nil {
		log.Debugf("Nix command failed: %v", err)
		return nil, fmt.Errorf("nix command failed: %w", err)
	}

	return bytes.TrimSpace(b.Bytes()), nil
}

func (c NixCommand) runWithReporter(ctx context.Context, cmd string, args []string) ([]byte, error) {
	sctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Add internal-json format flags
	args = append(args, "--log-format", "internal-json", "-v")

	// Create nix command
	nixc, err := c.exec.CommandContext(sctx, cmd, args...)
	if err != nil {
		return nil, err
	}

	// Create a buffer to capture nix's stdout
	b := &bytes.Buffer{}
	nixc.SetStdout(b)

	// Get stderr pipe from nix command
	stderr, err := nixc.StderrPipe()
	if err != nil {
		return nil, err
	}

	// Plug stdin if provided
	if c.stdin != nil {
		nixc.SetStdin(c.stdin)
	}

	// Start nix command
	if err = nixc.Start(); err != nil {
		log.Debugf("Failed to start nix command: %v", err)
		return nil, fmt.Errorf("failed to start nix command: %w", err)
	}

	// Create a goroutine pool
	p := pool.New().
		WithContext(sctx).
		WithCancelOnError()

	// Run progress reporter
	p.Go(func(ctx context.Context) error {
		return c.reporter.Run(ctx, NewProgressDecoder(stderr))
	})

	// Wait for nix command
	p.Go(func(ctx context.Context) error {
		return nixc.Wait()
	})

	// Wait for pool
	if err := p.Wait(); err != nil {
		log.Debugf("Nix command failed: %v", err)
		return nil, fmt.Errorf("nix command failed: %w", err)
	}

	return bytes.TrimSpace(b.Bytes()), nil
}

// CopyToTarget copies a Nix store path to a target host.
// If buildTarget is provided and different from target, copies from buildTarget.
// If buildTarget equals target, skips copy (already on target).
// If useReporter is true and reporter is provided, uses a progress reporter for the copy operation.
func CopyToTarget(ctx context.Context, builder exec.Executor, outPath, target, buildTarget string, useReporter bool, reporter ProgressReporter) error {
	// Skip copy if we built remotely on the same host as the deploy target
	if buildTarget != "" && buildTarget == target {
		log.Debugf("Skipping copy: build and deploy are on the same host (%s)", buildTarget)
		return nil
	}

	// Copy closure
	copyArgs := []string{
		"--to", fmt.Sprintf("ssh://%s", target),
	}

	// If we built remotely on a different host, copy from the build host
	if buildTarget != "" && buildTarget != target {
		buildUser, buildHostname := util.ParseTarget(buildTarget)
		buildStoreAddress := util.BuildStoreAddress(buildUser, buildHostname)
		copyArgs = append(copyArgs, "--from", buildStoreAddress)
		log.Debugf("Copying from build host %s to deploy target %s", buildTarget, target)
	}

	copyArgs = append(copyArgs, outPath)

	copyCmd := Command("copy").
		Args(copyArgs).
		Executor(builder)

	if useReporter && reporter != nil {
		copyCmd = copyCmd.Reporter(reporter)
	}

	_, err := copyCmd.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to copy to target: %w", err)
	}

	log.Debugf("Successfully copied to target")
	return nil
}
