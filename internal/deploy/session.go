package deploy

import (
	"context"

	"fmt"

	"github.com/arnarg/nilla-utils/internal/askpass"
	"github.com/arnarg/nilla-utils/internal/exec"
)

type SessionDeps struct {
	NewLocal   func() exec.Executor
	NewSSH     func(target string, cache *askpass.PasswordCache) (exec.Executor, error)
	NewAskpass func(cache *askpass.PasswordCache) (*askpass.Server, func(), error)
}

func DefaultDeps() SessionDeps {
	return SessionDeps{
		NewLocal:   func() exec.Executor { return exec.NewLocalExecutor() },
		NewSSH:     exec.NewSSHExecutor,
		NewAskpass: askpass.NewServer,
	}
}

type Session struct {
	Plan   *Plan
	System System

	local      exec.Executor
	target     exec.Executor
	forDiff    exec.Executor
	askpassSrv *askpass.Server
	pwCache    *askpass.PasswordCache
	cleanup    func()
	cancel     context.CancelFunc
}

func NewSession(ctx context.Context, plan *Plan, sys System, deps SessionDeps) (*Session, error) {
	ctx, cancel := context.WithCancel(ctx)

	// Create a new local executor
	local := deps.NewLocal()

	// Create a password cache for remote builds and/or deployments
	pwCache := askpass.NewPasswordCache()

	s := &Session{
		Plan:    plan,
		System:  sys,
		local:   local,
		pwCache: pwCache,
		cancel:  cancel,
	}

	// If we're about build on or deploy to a remote host we run the
	// askpass server
	if plan.BuildTarget != "" || plan.DeployTarget != "" {
		srv, cleanup, err := deps.NewAskpass(pwCache)
		if err != nil {
			cancel()
			return nil, err
		}
		s.askpassSrv = srv
		s.cleanup = cleanup
		go srv.Serve(ctx)
	}

	// Create a new SSH executor if the deploy target is remote
	if plan.DeployTarget != "" {
		target, err := deps.NewSSH(plan.DeployTarget, pwCache)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.target = target
	} else {
		s.target = local
	}

	// Get an executor for reading new generation for diff
	// comparison with previous generation
	forDiff, err := s.resolveDiffExecutor(deps)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.forDiff = forDiff

	return s, nil
}

func (s *Session) Close() {
	s.cancel()
	if s.cleanup != nil {
		s.cleanup()
	}
}

func (s *Session) BuildExecutor() exec.Executor {
	if s.askpassSrv != nil && s.Plan.StoreAddr != "" {
		return exec.NewAskpassExec(s.askpassSrv.SocketPath(), s.askpassSrv.Token(), "remote-build")
	}
	return s.local
}

func (s *Session) CopyExecutor() exec.Executor {
	if s.askpassSrv != nil {
		return exec.NewAskpassExec(s.askpassSrv.SocketPath(), s.askpassSrv.Token(), "copy-closure")
	}
	return s.local
}

func (s *Session) resolveDiffExecutor(deps SessionDeps) (exec.Executor, error) {
	p := s.Plan
	if p.BuildTarget != "" && p.StoreAddr != "" {
		if p.BuildTarget == p.DeployTarget {
			return s.target, nil
		}
		buildExec, err := deps.NewSSH(p.BuildTarget, s.pwCache)
		if err != nil {
			return nil, fmt.Errorf("failed to create SSH executor for build target: %w", err)
		}
		return buildExec, nil
	}
	if p.DeployTarget != "" {
		return s.local, nil
	}
	return s.local, nil
}
