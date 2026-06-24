package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// sweepMsg carries the current time to avoid calling time.Now() in Update.
type sweepMsg time.Time

func scheduleSweep() tea.Cmd {
	return tea.Tick(sweepInterval, func(t time.Time) tea.Msg {
		return sweepMsg(t)
	})
}

// baseModel holds shared state and behavior used by both buildModel
// and copyModel. Domain models embed it and add their own event
// handling, activeItems, and summary bar.
type baseModel struct {
	spinner spinner.Model

	w, h int

	maxLines int

	verbose     bool
	initialized bool
	done        bool

	list listState

	lastMsg string

	err error
}

func newBaseModel(maxLines int, verbose bool, initMsg string) baseModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return baseModel{
		spinner:  s,
		maxLines: maxLines,
		verbose:  verbose,
		list:     listState{done: []doneEntry{}},
		lastMsg:  initMsg,
	}
}

func (m baseModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, scheduleSweep())
}

func (m baseModel) error() error {
	return m.err
}

// updateCommon handles window resize, spinner ticks, and done-buffer
// sweeps. Returns (cmd, handled) so the domain model can fall through
// to its own event handling when not consumed.
func (m *baseModel) updateCommon(msg tea.Msg, activeCount int) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case finishedMsg:
		m.done = true
		return tea.Quit, true
	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.h = msg.Height
		return nil, true
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return cmd, true
	case sweepMsg:
		m.list = m.list.sweep(activeCount, listHeight(m.h, m.maxLines), time.Time(msg))
		return scheduleSweep(), true
	}
	return nil, false
}

func (m baseModel) errorView(msg string) string {
	return fmt.Sprintf("%s%s\n",
		m.spinner.View(),
		lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("1")).
			SetString(msg).
			String(),
	)
}

func (m baseModel) uninitializedView() string {
	return fmt.Sprintf("%s%s\n", m.spinner.View(), m.lastMsg)
}

// verboseList renders each active item with a spinner prefix.
func (m baseModel) verboseList(active []item) string {
	var b strings.Builder
	for _, i := range active {
		fmt.Fprintf(&b, "%s%s\n", m.spinner.View(), i.String())
	}
	return b.String()
}

// fallbackView returns the rendered list, or if empty, the last message
// (truncated to terminal width) with a spinner prefix.
func (m baseModel) fallbackView(rendered string) string {
	if rendered != "" || m.lastMsg == "" {
		return rendered
	}
	spin := m.spinner.View()
	width := m.w - lipgloss.Width(spin)
	msg := m.lastMsg
	if width > 0 && len(msg) > width {
		p := "..."
		l := (len(msg) - width) + len(p)
		msg = fmt.Sprintf("%s%s", p, m.lastMsg[l:])
	}
	return fmt.Sprintf("%s%s\n", spin, msg)
}
