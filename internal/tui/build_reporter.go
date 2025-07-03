package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type BuildReporter struct {
	verbose bool
}

func NewBuildReporter(verbose bool) *BuildReporter {
	return &BuildReporter{verbose}
}

func (r *BuildReporter) Run(ctx context.Context, decoder *nix.ProgressDecoder) error {
	return runTUIModel(ctx, initBuildModel(r.verbose), decoder)
}

func extractName(p string) string {
	return p[44:]
}

type build struct {
	name  string
	phase string
}

func (b *build) String() string {
	if b.phase != "" {
		return fmt.Sprintf("%s [%s]", b.name, b.phase)
	}
	return b.name
}

type buildModel struct {
	spinner spinner.Model

	verbose     bool
	initialized bool

	copyPathsProgs progresses
	buildsProgs    progresses

	downloads map[int64]*copy
	transfers map[int64]int64
	builds    map[int64]*build

	lastMsg string

	err error
}

func initBuildModel(verbose bool) buildModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return buildModel{
		verbose:        verbose,
		spinner:        s,
		copyPathsProgs: map[int64]progress{},
		buildsProgs:    map[int64]progress{},
		downloads:      map[int64]*copy{},
		builds:         map[int64]*build{},
		transfers:      map[int64]int64{},
		lastMsg:        "Initializing build...",
	}
}

func (m buildModel) error() error {
	return m.err
}

func (m buildModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m buildModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case nix.Event:
		return m.handleEvent(msg)
	}

	return m, nil
}

func (m buildModel) handleEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch ev.Action() {
	case nix.ActionTypeStart:
		return m.handleStartEvent(ev)
	case nix.ActionTypeStop:
		event := ev.(nix.StopEvent)
		return m.handleStopEvent(event)
	case nix.ActionTypeResult:
		return m.handleResultEvent(ev)
	case nix.ActionTypeMessage:
		event := ev.(nix.MessageEvent)

		// error
		if event.Level == 0 {
			// Lix does not have builtins.warn, this means warnings are logged at log level error
			traceWarning := strings.HasPrefix(event.Text, "trace: ") && strings.Contains(event.Text, "warning:")
			if !traceWarning {
				m.err = errors.New(event.Text)
				return m, nil
			}
		}

		// Just display the message
		if m.verbose {
			cmd = tea.Printf("%s", event.Text)
		} else {
			m.lastMsg = event.Text
		}
	}
	return m, cmd
}

func (m buildModel) handleStartEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case nix.StartCopyPathsEvent:
		m.copyPathsProgs[ev.ID] = progress{}
		if !m.initialized {
			m.initialized = true
		}
		return m, nil

	case nix.StartBuildsEvent:
		m.buildsProgs[ev.ID] = progress{}
		if !m.initialized {
			m.initialized = true
		}
		return m, nil

	case nix.StartCopyPathEvent:
		m.downloads[ev.ID] = &copy{name: extractName(ev.Path)}

		if m.verbose {
			return m, tea.Println(ev.Text)
		}
		return m, nil

	case nix.StartFileTransferEvent:
		if _, ok := m.downloads[ev.Parent]; !ok {
			return m, nil
		}

		m.transfers[ev.ID] = ev.Parent
		return m, nil

	case nix.StartBuildEvent:
		m.builds[ev.ID] = &build{name: strings.TrimSuffix(extractName(ev.Path), ".drv")}
		return m, nil
	}

	return m, nil
}

func (m buildModel) handleStopEvent(ev nix.StopEvent) (tea.Model, tea.Cmd) {
	// First check if ID is build
	if _, ok := m.builds[ev.ID]; ok {
		// Remove from builds map
		delete(m.builds, ev.ID)
	}

	// Then check if it's a download
	if _, ok := m.downloads[ev.ID]; ok {
		// Remove from downloads map
		delete(m.downloads, ev.ID)
	}

	// Finally we want to also clean up transfer parent mapping
	if _, ok := m.transfers[ev.ID]; ok {
		// Remove parent mapping
		delete(m.transfers, ev.ID)
	}

	// Clear last message if all builds and downloads have stopped,
	// but only after initialization
	if m.initialized && len(m.builds) < 1 && len(m.downloads) < 1 {
		m.lastMsg = ""
	}

	return m, nil
}

func (m buildModel) handleResultEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case nix.ResultSetPhaseEvent:
		b, ok := m.builds[ev.ID]
		if !ok {
			// Not found, ignore event
			return m, nil
		}

		b.phase = ev.Phase
		m.lastMsg = b.String()
		return m, nil

	case nix.ResultProgressEvent:
		// Check if the event ID is a CopyPaths or Builds event
		if cp, ok := m.copyPathsProgs[ev.ID]; ok {
			cp.done = int(ev.Done)
			cp.expected = int(ev.Expected)
			cp.running = ev.Running
			m.copyPathsProgs[ev.ID] = cp
			return m, nil
		} else if bp, ok := m.buildsProgs[ev.ID]; ok {
			bp.done = int(ev.Done)
			bp.expected = int(ev.Expected)
			bp.running = ev.Running
			m.buildsProgs[ev.ID] = bp
			return m, nil
		}

		// Otherwise we check if it's a transfer
		parent, ok := m.transfers[ev.ID]
		if !ok {
			return m, nil
		}

		d, ok := m.downloads[parent]
		if !ok {
			return m, nil
		}

		d.done = ev.Done
		d.total = ev.Expected

		m.lastMsg = d.String()
		return m, nil

	case nix.ResultBuildLogLineEvent:
		if m.verbose {
			// Try to find build
			if b, ok := m.builds[ev.ID]; ok {
				return m, tea.Printf(
					"%s %s",
					lipgloss.NewStyle().
						Foreground(lipgloss.Color("13")).
						SetString(fmt.Sprintf("%s>", b.name)).
						String(),
					ev.Text,
				)
			}
		}
	}

	return m, nil
}

func (m buildModel) View() string {
	if m.err != nil {
		return fmt.Sprintf(
			"%s%s\n",
			m.spinner.View(),
			lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("1")).
				SetString("Build failed! Exiting...").
				String(),
		)
	}

	if !m.initialized {
		return m.uninitializedView()
	}

	return m.progressView()
}

func (m buildModel) uninitializedView() string {
	return fmt.Sprintf("%s%s\n", m.spinner.View(), m.lastMsg)
}

type progressItem struct {
	id   int64
	text string
}

func (m buildModel) progressView() string {
	strb := &strings.Builder{}

	if m.verbose {
		items := []progressItem{}

		for id, d := range m.downloads {
			item := progressItem{
				id:   id,
				text: fmt.Sprintf("%s%s\n", m.spinner.View(), d.String()),
			}
			items = append(items, item)
		}

		for id, b := range m.builds {
			item := progressItem{
				id:   id,
				text: fmt.Sprintf("%s%s\n", m.spinner.View(), b.String()),
			}
			items = append(items, item)
		}

		slices.SortFunc(items, func(a, b progressItem) int {
			return cmp.Compare(a.id, b.id)
		})

		for _, i := range items {
			strb.WriteString(i.text)
		}
	} else {
		if m.lastMsg != "" {
			strb.WriteString(fmt.Sprintf("%s%s\n", m.spinner.View(), m.lastMsg))
		}
	}

	builds := fmtBuilds(m)
	downloads := fmtDownloads(m)

	bhdr := lipgloss.NewStyle().
		Bold(true).
		Width(lipgloss.Width(builds)).
		SetString("Builds:").
		String()
	dhdr := lipgloss.NewStyle().
		Bold(true).
		Width(lipgloss.Width(downloads)).
		SetString("Downloads:").
		String()

	strb.WriteString(fmt.Sprintf("%s | %s\n", bhdr, dhdr))
	strb.WriteString(fmt.Sprintf("%s | %s\n", builds, downloads))

	return strb.String()
}

func fmtBuilds(m buildModel) string {
	running := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		SetString(fmt.Sprintf("▶ %d", m.buildsProgs.totalRunning())).
		String()

	done := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		SetString(fmt.Sprintf("✓ %d", m.buildsProgs.totalDone())).
		String()

	remaining := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		SetString(fmt.Sprintf("⧗ %d", m.buildsProgs.totalExpected()-m.buildsProgs.totalDone())).
		String()

	return fmt.Sprintf("%s | %s | %s", running, done, remaining)
}

func fmtDownloads(m buildModel) string {
	running := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		SetString(fmt.Sprintf("↓ %d", m.copyPathsProgs.totalRunning())).
		String()

	done := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		SetString(fmt.Sprintf("✓ %d", m.copyPathsProgs.totalDone())).
		String()

	remaining := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		SetString(
			fmt.Sprintf(
				"⧗ %d", m.copyPathsProgs.totalExpected()-m.copyPathsProgs.totalDone(),
			),
		).
		String()

	return fmt.Sprintf("%s | %s | %s", running, done, remaining)
}
