package tui

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/util"
	tea "github.com/charmbracelet/bubbletea"
)

// Minimum time an item remains featured before rotating to the next
const minFeatureDuration = 100 * time.Millisecond

type tuiModel interface {
	tea.Model
	error() error
}

func runTUIModel(ctx context.Context, init tuiModel, decoder *nix.ProgressDecoder) error {
	var wg sync.WaitGroup

	p := tea.NewProgram(
		init,
		// Signal handling is done outside and
		// cancels ctx
		tea.WithoutSignalHandler(),
		// Output on stderr so that --print-out-paths
		// can print on stdout
		tea.WithOutput(os.Stderr),
		// Signal handling works outside of bubbletea
		// when input is nil
		tea.WithInput(nil),
		// Seems enough
		tea.WithFPS(30),
	)

	wg.Add(1)
	go func() {
		defer wg.Done()

		for ev := range decoder.Events {
			// Check if context has been cancelled
			select {
			case <-ctx.Done():
				p.Quit()
				return
			default:
			}

			// Send event to program
			p.Send(ev)
		}

		p.Quit()
	}()

	// Run bubbletea program
	m, err := p.Run()
	if err != nil {
		return err
	}

	// Wait for waitgroup
	wg.Wait()

	return m.(tuiModel).error()
}

type progress struct {
	id       int64
	done     int
	expected int
	running  int
}

type progresses map[int64]progress

func (p progresses) count() int {
	return len(p)
}

func (p progresses) totalDone() int {
	total := 0
	for _, prog := range p {
		total += prog.done
	}
	return total
}

func (p progresses) totalExpected() int {
	total := 0
	for _, prog := range p {
		total += prog.expected
	}
	return total
}

func (p progresses) totalRunning() int {
	total := 0
	for _, prog := range p {
		total += prog.running
	}
	return total
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

type copy struct {
	name  string
	done  int64
	total int64
}

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

// rotationCheckMsg carries the current time to avoid calling time.Now() in Update
type rotationCheckMsg time.Time

// featureMsg carries the timestamp from the command to keep Update pure
type featureMsg struct {
	id   int64
	kind string // "build" or "download"
	at   time.Time
}

// featureCmd captures the current time in a command (side-effect isolated)
func featureCmd(id int64, kind string) tea.Cmd {
	return func() tea.Msg {
		return featureMsg{id: id, kind: kind, at: time.Now()}
	}
}

// scheduleRotationCheck creates a delayed tick for the rotation checker
func scheduleRotationCheck() tea.Cmd {
	return tea.Tick(minFeatureDuration, func(t time.Time) tea.Msg {
		return rotationCheckMsg(t)
	})
}
