package tui

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/arnarg/nilla-utils/internal/nix"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	sweepInterval = 150 * time.Millisecond
	doneGrace     = 1 * time.Second
	summaryLines  = 3
	listHardMax   = 10
)

func listHeight(h, maxLines int) int {
	return max(1, min(h-summaryLines, maxLines))
}

type reporterModel interface {
	tea.Model
	error() error
}

// finishedMsg signals normal completion so the model can render a
// final summary-only view before the program exits.
type finishedMsg struct{}

func runTUIModel(ctx context.Context, init reporterModel, decoder *nix.ProgressDecoder) error {
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

		p.Send(finishedMsg{})
	}()

	// Run bubbletea program
	m, err := p.Run()
	if err != nil {
		return err
	}

	// Wait for waitgroup
	wg.Wait()

	return m.(reporterModel).error()
}
