package deploy

import (
	"context"
	"errors"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
)

// ErrNoCurrentGeneration is returned by System.CurrentGeneration when the
// target has no previous generation, e.g. on first activation.
var ErrNoCurrentGeneration = errors.New("no current generation found")

type Generation struct {
	Path    string
	Querier diff.StoreQuerier
}

type System interface {
	ResolveName(name string, projectPath string) (string, error)
	AttrPath(name string) string
	CurrentGeneration(executor exec.Executor, name string) (*Generation, error)
	Activate(ctx context.Context, executor exec.Executor, outPath string, cmd Command) error
}
