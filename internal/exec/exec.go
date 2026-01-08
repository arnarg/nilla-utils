package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/log"
)

type Executor interface {
	Command(string, ...string) (Command, error)
	CommandContext(context.Context, string, ...string) (Command, error)
	PathExists(string) (bool, error)
	IsLocal() bool
}

type Command interface {
	Run() error
	Start() error
	Wait() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.Reader, error)
	StderrPipe() (io.Reader, error)
}

// IsRootOnExecutor checks if the current user is root on the given executor.
// For local executors, uses util.IsRoot(). For remote executors, runs "id -u" and checks if it returns 0.
func IsRootOnExecutor(e Executor) (bool, error) {
	if e == nil || e.IsLocal() {
		return util.IsRoot(), nil
	}

	// Check if we're root on remote by running "id -u"
	cmd, err := e.Command("id", "-u")
	if err != nil {
		return false, err
	}

	var buf bytes.Buffer
	cmd.SetStdout(&buf)
	if err := cmd.Run(); err != nil {
		return false, err
	}

	uidStr := strings.TrimSpace(buf.String())
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return false, err
	}

	return uid == 0, nil
}

// CommandWithSudoIfNeeded creates a command with sudo prefix if not root, otherwise without.
// It automatically checks if the executor is root and only adds sudo if needed.
func CommandWithSudoIfNeeded(e Executor, cmd string, args ...string) (Command, error) {
	isRoot, err := IsRootOnExecutor(e)
	if err != nil {
		return nil, err
	}

	if isRoot {
		return e.Command(cmd, args...)
	}
	return e.Command("sudo", append([]string{cmd}, args...)...)
}

// CommandAsUser creates a command that runs as the specified user with HOME set correctly.
// It uses "sudo -u <user> -H" to switch to the user and set HOME to their home directory.
// If the executor is already running as the target user, it runs the command directly.
func CommandAsUser(e Executor, user string, cmd string, args ...string) (Command, error) {
	// Check if we're already running as the target user
	currentUser, err := getCurrentUser(e)
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	if currentUser == user {
		// Already running as target user, no sudo needed
		return e.Command(cmd, args...)
	}

	// Use sudo -u <user> -H to switch to user and set HOME from /etc/passwd
	sudoArgs := []string{"-u", user, "-H", cmd}
	sudoArgs = append(sudoArgs, args...)
	return e.Command("sudo", sudoArgs...)
}

// getCurrentUser gets the current username on the executor
func getCurrentUser(e Executor) (string, error) {
	if e == nil || e.IsLocal() {
		return util.GetUser(), nil
	}

	// Get username from remote executor
	cmd, err := e.Command("whoami")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	cmd.SetStdout(&buf)
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return strings.TrimSpace(buf.String()), nil
}

// DetermineNewBuildExecutor selects the executor for the new build based on build location and target.
// If we built remotely, use remote executor for the new build (faster - no copy needed).
// Otherwise, use local executor for the new build (it exists locally).
func DetermineNewBuildExecutor(builder, target Executor, buildTarget, storeAddress, targetStr string) (Executor, error) {
	if buildTarget != "" && storeAddress != "" {
		// Built remotely
		if buildTarget == targetStr && targetStr != "" {
			// Built on same host as deploy target
			log.Debugf("Using target executor for new build (built on target): %s", targetStr)
			return target, nil
		}
		// Built on different host - create executor for build host
		log.Debugf("Setting up SSH executor for build target: %s", buildTarget)
		buildExecutor, err := NewSSHExecutor(buildTarget)
		if err != nil {
			log.Debugf("Failed to create SSH executor for build target: %v", err)
			return nil, fmt.Errorf("failed to create SSH executor for build target: %w", err)
		}
		log.Debugf("Using build target executor for new build: %s", buildTarget)
		return buildExecutor, nil
	}

	// Built locally - use local executor (build output exists locally)
	log.Debugf("Using local executor for new build (built locally)")
	return builder, nil
}
