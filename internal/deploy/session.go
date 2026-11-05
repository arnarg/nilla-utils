package deploy

import (
	"context"
	"fmt"
	"io"

	"github.com/arnarg/nilla-utils/internal/exec"
)

type SessionDeps struct {
	NewLocal func() exec.Executor
	// NewSSH connects to a remote host. If the executor implements
	// io.Closer, the session closes it.
	NewSSH func(ctx context.Context, target string) (exec.Executor, error)
	// NixSSHEnv returns environment entries that make ssh processes started
	// by nix reuse the open connections ("" if none).
	NixSSHEnv func() string
}

func DefaultDeps() SessionDeps {
	return SessionDeps{
		NewLocal:  func() exec.Executor { return exec.NewLocalExecutor() },
		NewSSH:    exec.NewSSHExecutor,
		NixSSHEnv: exec.NixSSHOptsEnv,
	}
}

type Session struct {
	Plan   *Plan
	System System

	local   exec.Executor
	target  exec.Executor
	builder exec.Executor
	forDiff exec.Executor
	// nixRemote runs local nix commands that ssh to the build or deploy
	// host (nix copy, nix build --store ssh-ng://...).
	nixRemote exec.Executor
	closers   []io.Closer
	cancel    context.CancelFunc
}

// NewSession connects to the deploy target and the remote build host (if
// any) up front, so that any SSH prompts (passwords, passphrases, unknown
// host keys) happen before builds and progress output start. Later nix
// commands reuse these connections and never prompt.
func NewSession(ctx context.Context, plan *Plan, sys System, deps SessionDeps) (*Session, error) {
	ctx, cancel := context.WithCancel(ctx)

	s := &Session{
		Plan:   plan,
		System: sys,
		local:  deps.NewLocal(),
		cancel: cancel,
	}

	connect := func(target, role string) (exec.Executor, error) {
		e, err := deps.NewSSH(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("connecting to %s %s: %w", role, target, err)
		}
		if c, ok := e.(io.Closer); ok {
			s.closers = append(s.closers, c)
		}
		return e, nil
	}

	s.target = s.local
	if plan.DeployTarget != "" {
		target, err := connect(plan.DeployTarget, "deploy target")
		if err != nil {
			s.Close()
			return nil, err
		}
		s.target = target
	}

	if plan.BuildTarget != "" {
		if plan.BuildTarget == plan.DeployTarget {
			s.builder = s.target
		} else {
			builder, err := connect(plan.BuildTarget, "build host")
			if err != nil {
				s.Close()
				return nil, err
			}
			s.builder = builder
		}
	}

	s.nixRemote = s.local
	if deps.NixSSHEnv != nil {
		if env := deps.NixSSHEnv(); env != "" {
			s.nixRemote = exec.NewLocalExecutor(env)
		}
	}

	// The new generation is read where it was built.
	if !plan.NoDiff {
		s.forDiff = s.local
		if s.builder != nil && plan.StoreAddr != "" {
			s.forDiff = s.builder
		}
	}

	return s, nil
}

func (s *Session) Close() {
	s.cancel()
	for i := len(s.closers) - 1; i >= 0; i-- {
		s.closers[i].Close()
	}
	s.closers = nil
}

func (s *Session) BuildExecutor() exec.Executor {
	if s.Plan.StoreAddr != "" {
		return s.nixRemoteExecutor()
	}
	return s.local
}

func (s *Session) CopyExecutor() exec.Executor {
	return s.nixRemoteExecutor()
}

func (s *Session) nixRemoteExecutor() exec.Executor {
	if s.nixRemote != nil {
		return s.nixRemote
	}
	return s.local
}
