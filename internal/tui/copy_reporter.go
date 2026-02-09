package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type CopyReporter struct {
	verbose bool
}

func NewCopyReporter(verbose bool) *CopyReporter {
	return &CopyReporter{verbose}
}

func (r *CopyReporter) Run(ctx context.Context, decoder *nix.ProgressDecoder) error {
	return runTUIModel(ctx, initCopyModel(r.verbose), decoder)
}

type copyModel struct {
	spinner spinner.Model

	w, h int

	verbose     bool
	initialized bool

	copyPathsProgress progress
	transferProgress  transfer

	copies    copies
	transfers map[int64]int64

	lastMsg string

	err error

	// Featured item tracking (pure: times come from Msgs only)
	featuredID      int64
	featuredSince   time.Time
	rotationPending bool
}

func initCopyModel(verbose bool) copyModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return copyModel{
		verbose:           verbose,
		spinner:           s,
		copyPathsProgress: progress{id: 0},
		transferProgress:  transfer{id: 0},
		copies:            map[int64]*copy{},
		transfers:         map[int64]int64{},
		lastMsg:           "Initializing...",
		featuredID:        0,
		rotationPending:   false,
	}
}

func (m copyModel) error() error {
	return m.err
}

func (m copyModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.scheduleRotationCheck(),
	)
}

func (m copyModel) scheduleRotationCheck() tea.Cmd {
	return tea.Tick(minFeatureDuration, func(t time.Time) tea.Msg {
		return rotationCheckMsg(t)
	})
}

func (m copyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.h = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case rotationCheckMsg:
		now := time.Time(msg)
		m.rotationPending = false
		var cmd tea.Cmd

		// Rotate if minimum hold time passed
		if m.featuredID != 0 && now.Sub(m.featuredSince) >= minFeatureDuration {
			if nextID, ok := m.selectNextActive(); ok {
				m.featuredID = nextID
				m.featuredSince = now
				m.lastMsg = m.formatFeaturedItem()
			} else if _, exists := m.copies[m.featuredID]; !exists {
				// Current item gone, no replacement
				m.featuredID = 0
				if !m.verbose {
					m.lastMsg = ""
				}
			}
		}

		// Reschedule if work remains
		if len(m.copies) > 0 {
			m.rotationPending = true
			cmd = m.scheduleRotationCheck()
		}

		return m, cmd

	case featureMsg:
		// New item featured (from start or immediate rotation)
		m.featuredID = msg.id
		m.featuredSince = msg.at
		m.lastMsg = m.formatFeaturedItem()
		return m, nil

	case nix.Event:
		return m.handleEvent(msg)
	}

	return m, nil
}

// selectNextActive is pure: no side effects, no time calls
func (m copyModel) selectNextActive() (int64, bool) {
	currentID := m.featuredID

	// Simple round-robin: pick any other active copy
	for id := range m.copies {
		if id != currentID {
			return id, true
		}
	}

	// If current still exists, keep it
	if _, exists := m.copies[currentID]; exists {
		return currentID, true
	}

	return 0, false
}

func (m copyModel) formatFeaturedItem() string {
	if c, ok := m.copies[m.featuredID]; ok {
		return c.String()
	}
	return ""
}

func (m copyModel) handleEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev.Action() {
	case nix.ActionTypeStart:
		return m.handleStartEvent(ev)
	case nix.ActionTypeStop:
		return m.handleStopEvent(ev.(nix.StopEvent))
	case nix.ActionTypeResult:
		return m.handleResultEvent(ev)
	case nix.ActionTypeMessage:
		return m.handleMessageEvent(ev.(nix.MessageEvent))
	}
	return m, nil
}

func (m copyModel) handleStartEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case nix.StartCopyPathsEvent:
		m.copyPathsProgress = progress{id: ev.ID}
		m.transferProgress = transfer{id: ev.ID}
		if !m.initialized {
			m.initialized = true
		}
		return m, nil

	case nix.StartCopyPathEvent:
		m.copies[ev.ID] = &copy{name: ev.Path}

		// Feature immediately if nothing else featured (via command for purity)
		if m.featuredID == 0 {
			// Empty string for kind since copies only have one type
			return m, featureCmd(ev.ID, "")
		}

		if m.verbose {
			return m, tea.Println(ev.Text)
		}
		return m, nil

	case nix.StartFileTransferEvent:
		if _, ok := m.copies[ev.Parent]; !ok {
			return m, nil
		}
		m.transfers[ev.ID] = ev.Parent
		return m, nil
	}

	return m, nil
}

func (m copyModel) handleStopEvent(ev nix.StopEvent) (tea.Model, tea.Cmd) {
	// Check if it's a copy
	if c, ok := m.copies[ev.ID]; ok {
		m.transferProgress.done += c.total
		delete(m.copies, ev.ID)

		if m.verbose {
			m.lastMsg = ""
			return m, tea.Printf(
				"%s %s",
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("10")).
					SetString("✓").
					String(),
				c.name,
			)
		}
	}

	// Clean up transfer parent mapping
	if _, ok := m.transfers[ev.ID]; ok {
		delete(m.transfers, ev.ID)
	}

	// If featured item stopped, rotate immediately
	if m.featuredID == ev.ID {
		if nextID, ok := m.selectNextActive(); ok {
			return m, featureCmd(nextID, "")
		}
		m.featuredID = 0
		if !m.verbose {
			m.lastMsg = ""
		}
	}

	// Clear last message if all done
	if m.initialized && len(m.copies) < 1 {
		m.lastMsg = ""
	}

	return m, nil
}

func (m copyModel) handleResultEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case nix.ResultProgressEvent:
		// Check if the event ID is a CopyPaths aggregate event
		if ev.ID == m.copyPathsProgress.id {
			m.copyPathsProgress.done = int(ev.Done)
			m.copyPathsProgress.expected = int(ev.Expected)
			m.copyPathsProgress.running = ev.Running
			return m, nil
		}

		// Otherwise check if it's a transfer
		var c *copy
		if co, ok := m.copies[ev.ID]; ok {
			c = co
		} else if parent, ok := m.transfers[ev.ID]; ok {
			co, ok := m.copies[parent]
			if !ok {
				return m, nil
			}
			c = co
		}

		if c != nil {
			c.done = ev.Done
			c.total = ev.Expected

			// Only update display if this is the featured copy
			if m.featuredID == ev.ID || (c == m.copies[m.featuredID]) {
				m.lastMsg = c.String()
			}
		}

	case nix.ResultSetExpectedCopyPathEvent:
		if ev.ID == m.transferProgress.id {
			m.transferProgress.expected = ev.Expected
			return m, nil
		}
	}
	return m, nil
}

func (m copyModel) handleMessageEvent(ev nix.MessageEvent) (tea.Model, tea.Cmd) {
	if ev.Level == 0 {
		m.err = errors.New(ev.Text)
		return m, nil
	}

	if m.verbose {
		return m, tea.Printf("%s", ev.Text)
	}

	m.lastMsg = ev.Text
	return m, nil
}

func (m copyModel) View() string {
	if m.err != nil {
		return fmt.Sprintf(
			"%s%s\n",
			m.spinner.View(),
			lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("1")).
				SetString("Copying failed! Exiting...").
				String(),
		)
	}

	if !m.initialized {
		return m.uninitializedView()
	}

	return m.progressView()
}

func (m copyModel) uninitializedView() string {
	return fmt.Sprintf("%s%s\n", m.spinner.View(), m.lastMsg)
}

func (m copyModel) progressView() string {
	strb := &strings.Builder{}

	if m.lastMsg != "" {
		width := m.w - lipgloss.Width(m.spinner.View())
		msg := m.lastMsg
		if len(msg) > width {
			p := "..."
			l := (len(msg) - width) + len(p)
			msg = fmt.Sprintf("%s%s", p, msg[l:])
		}
		fmt.Fprintf(strb, "%s%s\n", m.spinner.View(), msg)
	}

	transfers := fmtTransfers(m)

	hdr := lipgloss.NewStyle().
		Bold(true).
		Width(lipgloss.Width(transfers)).
		SetString("Transfers:").
		String()

	fmt.Fprintf(strb, "%s\n", hdr)
	fmt.Fprintf(strb, "%s\n", transfers)

	return strb.String()
}

func fmtTransfers(m copyModel) string {
	running := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		SetString(fmt.Sprintf("↑ %d", m.copyPathsProgress.running)).
		String()

	done := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		SetString(fmt.Sprintf("✓ %d", m.copyPathsProgress.done)).
		String()

	remaining := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		SetString(
			fmt.Sprintf(
				"⧗ %d", m.copyPathsProgress.expected-m.copyPathsProgress.done-m.copyPathsProgress.running,
			),
		).
		String()

	rTotal, unit := util.ConvertBytes(m.transferProgress.expected)
	rDone := util.ConvertBytesToUnit(m.transferProgress.done+m.copies.done(), unit)
	rProgress := fmt.Sprintf("[%.2f/%.2f %s]", rDone, rTotal, unit)

	return fmt.Sprintf("%s | %s | %s %s", running, done, remaining, rProgress)
}
