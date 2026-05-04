package deploy

import (
	"context"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
)

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
