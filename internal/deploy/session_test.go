package deploy

import (
	"context"
	"fmt"
	"testing"

	"github.com/arnarg/nilla-utils/internal/askpass"
	"github.com/arnarg/nilla-utils/internal/exec"
)

type mockExecutor struct {
	isLocal bool
}

func (m *mockExecutor) Command(string, ...string) (exec.Command, error) { return nil, nil }
func (m *mockExecutor) CommandContext(context.Context, string, ...string) (exec.Command, error) {
	return nil, nil
}
func (m *mockExecutor) PathExists(string) (bool, error) { return false, nil }
func (m *mockExecutor) IsLocal() bool                   { return m.isLocal }

func TestNewSession_localOnly(t *testing.T) {
	local := &mockExecutor{isLocal: true}
	deps := SessionDeps{
		NewLocal:   func() exec.Executor { return local },
		NewAskpass: askpass.NewServer,
	}

	s, err := NewSession(context.Background(), &Plan{SubCmd: Build}, NixOSSystem{}, deps)
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
	if s.askpassSrv != nil {
		t.Error("askpassSrv should be nil for local-only")
	}
}

func TestNewSession_withDeployTarget(t *testing.T) {
	local := &mockExecutor{isLocal: true}
	target := &mockExecutor{isLocal: false}

	sshCalls := []string{}
	deps := SessionDeps{
		NewLocal: func() exec.Executor { return local },
		NewSSH: func(tgt string, cache *askpass.PasswordCache) (exec.Executor, error) {
			sshCalls = append(sshCalls, tgt)
			return target, nil
		},
		NewAskpass: askpass.NewServer,
	}

	plan := &Plan{
		SubCmd:       Switch,
		DeployTarget: "user@deployhost",
	}

	s, err := NewSession(context.Background(), plan, NixOSSystem{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if s.target != target {
		t.Error("target should be SSH executor")
	}
	if s.forDiff != local {
		t.Error("forDiff should be local when built locally and deployed remotely")
	}
	if len(sshCalls) != 1 || sshCalls[0] != "user@deployhost" {
		t.Errorf("expected 1 NewSSH call for user@deployhost, got %v", sshCalls)
	}
}

func TestNewSession_remoteBuildSameAsDeploy(t *testing.T) {
	local := &mockExecutor{isLocal: true}
	target := &mockExecutor{isLocal: false}

	sshCalls := []string{}
	deps := SessionDeps{
		NewLocal: func() exec.Executor { return local },
		NewSSH: func(tgt string, cache *askpass.PasswordCache) (exec.Executor, error) {
			sshCalls = append(sshCalls, tgt)
			return target, nil
		},
		NewAskpass: askpass.NewServer,
	}

	plan := &Plan{
		SubCmd:       Switch,
		BuildTarget:  "user@host",
		DeployTarget: "user@host",
		StoreAddr:    "ssh-ng://user@host",
	}

	s, err := NewSession(context.Background(), plan, NixOSSystem{}, deps)
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
	if len(sshCalls) != 1 {
		t.Errorf("expected 1 NewSSH call, got %d", len(sshCalls))
	}
}

func TestNewSession_remoteBuildDifferentFromDeploy(t *testing.T) {
	deployExec := &mockExecutor{isLocal: false}
	buildExec := &mockExecutor{isLocal: false}

	sshCalls := []string{}
	sshResults := map[string]exec.Executor{
		"deployuser@deployhost": deployExec,
		"builduser@builder":     buildExec,
	}

	deps := SessionDeps{
		NewLocal: func() exec.Executor { return &mockExecutor{isLocal: true} },
		NewSSH: func(tgt string, cache *askpass.PasswordCache) (exec.Executor, error) {
			sshCalls = append(sshCalls, tgt)
			return sshResults[tgt], nil
		},
		NewAskpass: askpass.NewServer,
	}

	plan := &Plan{
		SubCmd:       Switch,
		BuildTarget:  "builduser@builder",
		DeployTarget: "deployuser@deployhost",
		StoreAddr:    "ssh-ng://builduser@builder",
	}

	s, err := NewSession(context.Background(), plan, NixOSSystem{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if s.target != deployExec {
		t.Error("target should be deploy executor")
	}
	if s.forDiff != buildExec {
		t.Error("forDiff should be build executor when build and deploy are different hosts")
	}
	if len(sshCalls) != 2 {
		t.Errorf("expected 2 NewSSH calls, got %d: %v", len(sshCalls), sshCalls)
	}
}

func TestNewSession_sshError(t *testing.T) {
	cleanupCalled := false

	deps := SessionDeps{
		NewLocal: func() exec.Executor { return &mockExecutor{isLocal: true} },
		NewSSH: func(tgt string, cache *askpass.PasswordCache) (exec.Executor, error) {
			return nil, fmt.Errorf("SSH failed")
		},
		NewAskpass: func(cache *askpass.PasswordCache) (*askpass.Server, func(), error) {
			srv, cleanup, err := askpass.NewServer(cache)
			if err != nil {
				return nil, nil, err
			}
			return srv, func() {
				cleanup()
				cleanupCalled = true
			}, nil
		},
	}

	_, err := NewSession(context.Background(), &Plan{
		SubCmd:       Switch,
		DeployTarget: "user@host",
	}, NixOSSystem{}, deps)
	if err == nil {
		t.Fatal("expected error from NewSession")
	}
	if !cleanupCalled {
		t.Error("expected cleanup to be called on SSH failure")
	}
}

func TestNewSession_askpassError(t *testing.T) {
	deps := SessionDeps{
		NewLocal: func() exec.Executor { return &mockExecutor{isLocal: true} },
		NewAskpass: func(cache *askpass.PasswordCache) (*askpass.Server, func(), error) {
			return nil, nil, fmt.Errorf("askpass failed")
		},
	}

	_, err := NewSession(context.Background(), &Plan{
		SubCmd:       Switch,
		DeployTarget: "user@host",
	}, NixOSSystem{}, deps)
	if err == nil {
		t.Fatal("expected error from NewSession")
	}
}

func TestNewSession_diffExecutorSSHError(t *testing.T) {
	deps := SessionDeps{
		NewLocal: func() exec.Executor { return &mockExecutor{isLocal: true} },
		NewSSH: func(tgt string, cache *askpass.PasswordCache) (exec.Executor, error) {
			if tgt == "deployuser@deployhost" {
				return &mockExecutor{isLocal: false}, nil
			}
			return nil, fmt.Errorf("SSH failed for %s", tgt)
		},
		NewAskpass: askpass.NewServer,
	}

	_, err := NewSession(context.Background(), &Plan{
		SubCmd:       Switch,
		BuildTarget:  "builduser@builder",
		DeployTarget: "deployuser@deployhost",
		StoreAddr:    "ssh-ng://builduser@builder",
	}, NixOSSystem{}, deps)
	if err == nil {
		t.Fatal("expected error from NewSession when diff executor SSH fails")
	}
}

func TestSession_BuildExecutor(t *testing.T) {
	t.Run("local build returns local executor", func(t *testing.T) {
		local := &mockExecutor{isLocal: true}
		s := &Session{
			Plan:  &Plan{},
			local: local,
		}
		if s.BuildExecutor() != local {
			t.Error("expected local executor for local build")
		}
	})

	t.Run("remote build with askpass returns askpass executor", func(t *testing.T) {
		srv, cleanup, err := askpass.NewServer(askpass.NewPasswordCache())
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()

		local := &mockExecutor{isLocal: true}
		s := &Session{
			Plan:       &Plan{StoreAddr: "ssh-ng://user@host"},
			local:      local,
			askpassSrv: srv,
		}

		be := s.BuildExecutor()
		if be == local {
			t.Error("expected askpass executor, not local")
		}
		if !be.IsLocal() {
			t.Error("askpass executor should report as local")
		}
	})

	t.Run("has askpass but no store addr returns local executor", func(t *testing.T) {
		srv, cleanup, err := askpass.NewServer(askpass.NewPasswordCache())
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()

		local := &mockExecutor{isLocal: true}
		s := &Session{
			Plan:       &Plan{},
			local:      local,
			askpassSrv: srv,
		}

		if s.BuildExecutor() != local {
			t.Error("expected local executor when store addr is empty")
		}
	})
}

func TestSession_CopyExecutor(t *testing.T) {
	t.Run("no askpass returns local executor", func(t *testing.T) {
		local := &mockExecutor{isLocal: true}
		s := &Session{
			Plan:  &Plan{},
			local: local,
		}
		if s.CopyExecutor() != local {
			t.Error("expected local executor when no askpass")
		}
	})

	t.Run("has askpass returns askpass executor", func(t *testing.T) {
		srv, cleanup, err := askpass.NewServer(askpass.NewPasswordCache())
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()

		local := &mockExecutor{isLocal: true}
		s := &Session{
			Plan:       &Plan{},
			local:      local,
			askpassSrv: srv,
		}

		ce := s.CopyExecutor()
		if ce == local {
			t.Error("expected askpass executor, not local")
		}
	})
}
