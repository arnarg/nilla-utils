package tui

import (
	"fmt"
	"time"

	"github.com/arnarg/nilla-utils/internal/util"
)

type ReporterMode int

const (
	ReporterModeNormal ReporterMode = iota
	ReporterModeCompact
	ReporterModeVerbose
)

func ResolveReporterMode(compact, verbose bool) ReporterMode {
	if verbose {
		return ReporterModeVerbose
	} else if compact {
		return ReporterModeCompact
	}

	return ReporterModeNormal
}

// item is a trackable entry that can appear in the active list or the
// done buffer. orderKey provides a stable, monotonic screen position.
type item interface {
	orderKey() int64
	fmt.Stringer
}

// doneEntry is a finished item retained in the done buffer so the view
// shrinks gracefully instead of collapsing all at once.
type doneEntry struct {
	item
	finishedAt time.Time
}

type progress struct {
	id       int64
	done     int
	expected int
	running  int
}

type transfer struct {
	id       int64
	done     int64
	expected int64
}

func (r transfer) String() string {
	total, unit := util.ConvertBytes(r.expected)
	done := util.ConvertBytesToUnit(r.done, unit)

	return fmt.Sprintf("[%.2f/%.2f %s]", done, total, unit)
}

type build struct {
	id    int64
	name  string
	phase string
}

func (b *build) orderKey() int64 { return b.id }

func (b *build) String() string {
	if b.phase != "" {
		return fmt.Sprintf("%s [%s]", b.name, b.phase)
	}
	return b.name
}

type copy struct {
	id    int64
	name  string
	done  int64
	total int64
}

func (c *copy) orderKey() int64 { return c.id }

func (c *copy) String() string {
	if c.total > 0 {
		total, unit := util.ConvertBytes(c.total)
		done := util.ConvertBytesToUnit(c.done, unit)

		return fmt.Sprintf("%s [%.2f/%.2f %s]", c.name, done, total, unit)
	}
	return c.name
}

type copies map[int64]*copy

func (c copies) done() int64 {
	total := int64(0)
	for _, copy := range c {
		total += copy.done
	}
	return total
}

func (c copies) total() int64 {
	total := int64(0)
	for _, copy := range c {
		total += copy.total
	}
	return total
}

func (c copies) String() string {
	total, unit := util.ConvertBytes(c.total())
	done := util.ConvertBytesToUnit(c.done(), unit)

	return fmt.Sprintf("[%.2f/%.2f %s]", done, total, unit)
}
