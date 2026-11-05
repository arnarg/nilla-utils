package deploy

import (
	"context"
	"fmt"
	"testing"

	"github.com/arnarg/nilla-utils/internal/exec"
)

type mockExecutor struct {
	isLocal bool
	closed  bool
}

func (m *mockExecutor) Command(string, ...string) (exec.Command, error) { return nil, nil }
func (m *mockExecutor) CommandContext(context.Context, string, ...string) (exec.Command, error) {
	return nil, nil
}
func (m *mockExecutor) PathExists(string) (bool, error) { return false, nil }
func (m *mockExecutor) IsLocal() bool                   { return m.isLocal }
func (m *mockExecutor) Close() error {
	m.closed = true
	return nil
}

// sshDeps returns deps whose NewSSH hands out the given executors (or an
// error for targets not in the map) and records the targets it was asked for.
func sshDeps(local exec.Executor, remotes map[string]exec.Executor, calls *[]string) SessionDeps {
	return SessionDeps{
		NewLocal: func() exec.Executor { return local },
		NewSSH: func(_ context.Context, tgt string) (exec.Executor, error) {
			*calls = append(*calls, tgt)
			if e, ok := remotes[tgt]; ok {
				return e, nil
			}
			return nil, fmt.Errorf("SSH failed for %s", tgt)
		},
		NixSSHEnv: func() string {
			if len(*calls) == 0 {
				return ""
			}
			return "NIX_SSHOPTS=-o ControlPath=/tmp/x/%C"
		},
	}
}

func TestNewSession_localOnly(t *testing.T) {
	local := &mockExecutor{isLocal: true}
	var calls []string

	s, err := NewSession(context.Background(), &Plan{SubCmd: Build}, NixOSSystem{}, sshDeps(local, nil, &calls))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if s.target != local {
		t.Error("target should be local executor")
	}
	if s.forDiff != local {
		t.Error("forDiff should be local executor")
	}
	if len(calls) != 0 {
		t.Errorf("expected no SSH connections, got %v", calls)
	}
	if s.CopyExecutor() != local || s.BuildExecutor() != local {
		t.Error("nix commands should run with the plain local executor")
	}
}

func TestNewSession_withDeployTarget(t *testing.T) {
	local := &mockExecutor{isLocal: true}
	target := &mockExecutor{}
	var calls []string

	plan := &Plan{
		SubCmd:       Switch,
		DeployTarget: "user@deployhost",
	}

	s, err := NewSession(context.Background(), plan, NixOSSystem{}, sshDeps(local, map[string]exec.Executor{"user@deployhost": target}, &calls))
	if err != nil {
		t.Fatal(err)
	}

	if s.target != target {
		t.Error("target should be SSH executor")
	}
	if s.forDiff != local {
		t.Error("forDiff should be local when built locally and deployed remotely")
	}
	if len(calls) != 1 || calls[0] != "user@deployhost" {
		t.Errorf("expected 1 NewSSH call for user@deployhost, got %v", calls)
	}
	if s.CopyExecutor() == local {
		t.Error("copy should run with NIX_SSHOPTS set")
	}

	s.Close()
	if !target.closed {
		t.Error("Close should close the SSH connection")
	}
}

func TestNewSession_remoteBuildSameAsDeploy(t *testing.T) {
	local := &mockExecutor{isLocal: true}
	target := &mockExecutor{}
	var calls []string

	plan := &Plan{
		SubCmd:       Switch,
		BuildTarget:  "user@host",
		DeployTarget: "user@host",
		StoreAddr:    "ssh-ng://user@host",
	}

	s, err := NewSession(context.Background(), plan, NixOSSystem{}, sshDeps(local, map[string]exec.Executor{"user@host": target}, &calls))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if s.target != target {
		t.Error("target should be SSH executor")
	}
	if s.forDiff != target {
		t.Error("forDiff should be target when build and deploy are same host")
	}
	if len(calls) != 1 {
		t.Errorf("expected 1 NewSSH call, got %v", calls)
	}
	if s.BuildExecutor() == local {
		t.Error("remote build should run with NIX_SSHOPTS set")
	}
}

func TestNewSession_remoteBuildDifferentFromDeploy(t *testing.T) {
	deployExec := &mockExecutor{}
	buildExec := &mockExecutor{}
	var calls []string

	plan := &Plan{
		SubCmd:       Switch,
		BuildTarget:  "builduser@builder",
		DeployTarget: "deployuser@deployhost",
		StoreAddr:    "ssh-ng://builduser@builder",
	}

	s, err := NewSession(context.Background(), plan, NixOSSystem{}, sshDeps(&mockExecutor{isLocal: true}, map[string]exec.Executor{
		"deployuser@deployhost": deployExec,
		"builduser@builder":     buildExec,
	}, &calls))
	if err != nil {
		t.Fatal(err)
	}

	if s.target != deployExec {
		t.Error("target should be deploy executor")
	}
	if s.forDiff != buildExec {
		t.Error("forDiff should be build executor when build and deploy are different hosts")
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 NewSSH calls, got %v", calls)
	}

	s.Close()
	if !deployExec.closed || !buildExec.closed {
		t.Error("Close should close both SSH connections")
	}
}

// The build host is connected up front even with --no-diff: nix's ssh
// processes need the shared connection, and prompts must happen before the
// build starts.
func TestNewSession_noDiffStillConnectsToBuildHost(t *testing.T) {
	var calls []string
	plan := &Plan{
		SubCmd:       Switch,
		BuildTarget:  "builduser@builder",
		DeployTarget: "deployuser@deployhost",
		StoreAddr:    "ssh-ng://builduser@builder",
		NoDiff:       true,
	}

	s, err := NewSession(context.Background(), plan, NixOSSystem{}, sshDeps(&mockExecutor{isLocal: true}, map[string]exec.Executor{
		"deployuser@deployhost": &mockExecutor{},
		"builduser@builder":     &mockExecutor{},
	}, &calls))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if s.forDiff != nil {
		t.Error("forDiff should be nil when NoDiff is set")
	}
	if len(calls) != 2 {
		t.Errorf("expected connections to both hosts, got %v", calls)
	}
}

func TestNewSession_sshError(t *testing.T) {
	var calls []string
	_, err := NewSession(context.Background(), &Plan{
		SubCmd:       Switch,
		DeployTarget: "user@host",
	}, NixOSSystem{}, sshDeps(&mockExecutor{isLocal: true}, nil, &calls))
	if err == nil {
		t.Fatal("expected error from NewSession")
	}
}

func TestNewSession_buildHostErrorClosesDeployConnection(t *testing.T) {
	deployExec := &mockExecutor{}
	var calls []string

	_, err := NewSession(context.Background(), &Plan{
		SubCmd:       Switch,
		BuildTarget:  "builduser@builder",
		DeployTarget: "deployuser@deployhost",
		StoreAddr:    "ssh-ng://builduser@builder",
	}, NixOSSystem{}, sshDeps(&mockExecutor{isLocal: true}, map[string]exec.Executor{
		"deployuser@deployhost": deployExec,
	}, &calls))
	if err == nil {
		t.Fatal("expected error when the build host connection fails")
	}
	if !deployExec.closed {
		t.Error("deploy connection should be closed when NewSession fails")
	}
}

func TestSession_BuildExecutor(t *testing.T) {
	local := &mockExecutor{isLocal: true}
	remote := &mockExecutor{isLocal: true}

	t.Run("local build returns local executor", func(t *testing.T) {
		s := &Session{Plan: &Plan{}, local: local, nixRemote: remote}
		if s.BuildExecutor() != local {
			t.Error("expected local executor for local build")
		}
	})

	t.Run("remote build uses the NIX_SSHOPTS executor", func(t *testing.T) {
		s := &Session{Plan: &Plan{StoreAddr: "ssh-ng://user@host"}, local: local, nixRemote: remote}
		if s.BuildExecutor() != remote {
			t.Error("expected NIX_SSHOPTS executor for remote build")
		}
	})
}
