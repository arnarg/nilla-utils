package generation

import (
	"context"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
)

// Generation is the common representation of a NixOS or Home Manager generation.
// KernelVersion is only populated for NixOS generations.
type Generation struct {
	ID            int
	BuildDate     time.Time
	Version       string
	KernelVersion string

	path string
}

// Path returns the filesystem path of the generation profile link.
func (g Generation) Path() string { return g.path }

// System abstracts the differences between NixOS and Home Manager generations so
// that listing, deletion and garbage collection can be driven generically.
type System interface {
	Current(h exec.Host) (Generation, error)
	List(h exec.Host) ([]Generation, error)
	DeleteGenerations(h exec.Host, gens []Generation) error
	CollectGarbage(ctx context.Context, h exec.Host) error
	Headers() []string
	Row(g Generation) []string
	RequiresLocalRoot() bool
}
