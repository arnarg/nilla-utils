package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/util"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type BuildReporter struct {
	maxLines int
	verbose  bool
}

func NewBuildReporter(mode ReporterMode) *BuildReporter {
	reporter := &BuildReporter{
		maxLines: listHardMax,
	}

	switch mode {
	case ReporterModeCompact:
		reporter.maxLines = 1
	case ReporterModeVerbose:
		reporter.verbose = true
	default:
	}

	return reporter
}

func (r *BuildReporter) Run(ctx context.Context, decoder *nix.ProgressDecoder) error {
	m := buildModel{
		baseModel:         newBaseModel(r.maxLines, r.verbose, "Initializing build..."),
		buildProgress:     progress{id: 0},
		copyPathsProgress: progress{id: 0},
		realiseProgress:   transfer{id: 0},
		downloads:         map[int64]*copy{},
		builds:            map[int64]*build{},
		transfers:         map[int64]int64{},
	}
	return runTUIModel(ctx, m, decoder)
}

func extractName(p string) string {
	return p[44:]
}

type buildModel struct {
	baseModel

	buildProgress     progress
	copyPathsProgress progress
	realiseProgress   transfer

	downloads copies
	transfers map[int64]int64
	builds    map[int64]*build
}

func (m buildModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.updateCommon(msg, len(m.builds)+len(m.downloads)); handled {
		return m, cmd
	}
	if ev, ok := msg.(nix.Event); ok {
		return m.handleEvent(ev)
	}
	return m, nil
}

func (m buildModel) activeItems() []item {
	items := make([]item, 0, len(m.builds)+len(m.downloads))
	for _, b := range m.builds {
		items = append(items, b)
	}
	for _, d := range m.downloads {
		items = append(items, d)
	}
	slices.SortFunc(items, func(a, b item) int {
		return cmp.Compare(b.orderKey(), a.orderKey())
	})
	return items
}

func (m buildModel) handleEvent(ev nix.Event) (tea.Model, tea.Cmd) {
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

func (m buildModel) handleStartEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case nix.StartCopyPathsEvent:
		m.copyPathsProgress = progress{id: ev.ID}
		m.initialized = true
		return m, nil

	case nix.StartBuildsEvent:
		m.buildProgress = progress{id: ev.ID}
		m.initialized = true
		return m, nil

	case nix.StartRealiseEvent:
		m.realiseProgress = transfer{id: ev.ID}
		return m, nil

	case nix.StartCopyPathEvent:
		m.downloads[ev.ID] = &copy{id: ev.ID, name: extractName(ev.Path)}
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
		m.builds[ev.ID] = &build{id: ev.ID, name: strings.TrimSuffix(extractName(ev.Path), ".drv")}
		return m, nil
	}

	return m, nil
}

func (m buildModel) handleStopEvent(ev nix.StopEvent) (tea.Model, tea.Cmd) {
	delete(m.transfers, ev.ID)

	if b, ok := m.builds[ev.ID]; ok {
		m.list = m.list.add(b)
		delete(m.builds, ev.ID)
	}

	if d, ok := m.downloads[ev.ID]; ok {
		m.realiseProgress.done += d.total
		m.list = m.list.add(d)
		delete(m.downloads, ev.ID)
	}

	if m.initialized && len(m.builds) < 1 && len(m.downloads) < 1 {
		m.lastMsg = ""
	}

	return m, nil
}

func (m buildModel) handleResultEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case nix.ResultSetPhaseEvent:
		if b, ok := m.builds[ev.ID]; ok {
			b.phase = ev.Phase
		}
		return m, nil

	case nix.ResultProgressEvent:
		if ev.ID == m.copyPathsProgress.id {
			m.copyPathsProgress.done = int(ev.Done)
			m.copyPathsProgress.expected = int(ev.Expected)
			m.copyPathsProgress.running = ev.Running
			return m, nil
		}
		if ev.ID == m.buildProgress.id {
			m.buildProgress.done = int(ev.Done)
			m.buildProgress.expected = int(ev.Expected)
			m.buildProgress.running = ev.Running
			return m, nil
		}

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
		return m, nil

	case nix.ResultSetExpectedFileTransferEvent:
		if ev.ID == m.realiseProgress.id {
			m.realiseProgress.expected = ev.Expected
			return m, nil
		}

	case nix.ResultBuildLogLineEvent:
		if m.verbose {
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

func (m buildModel) handleMessageEvent(ev nix.MessageEvent) (tea.Model, tea.Cmd) {
	// Detect trace warnings (Lix sends all messages with error level 0)
	isTraceWarning := strings.HasPrefix(ev.Text, "trace: ") &&
		strings.Contains(ev.Text, "warning:")

	// Level 0 errors that aren't trace warnings are actual errors
	if ev.Level == nix.MsgLevelError && !isTraceWarning {
		m.err = errors.Join(m.err, errors.New(ev.Text))
		return m, nil
	}

	// Print immediately for: verbose mode, warnings (level 1), or trace warnings
	if m.verbose || ev.Level == nix.MsgLevelWarning || isTraceWarning {
		return m, tea.Printf("%s", util.TrimSpaceAnsi(ev.Text))
	}

	// Non-verbose, non-warning messages: display next to spinner
	m.lastMsg = ev.Text
	return m, nil
}

func (m buildModel) View() string {
	if m.err != nil {
		return m.errorView("Build failed! Exiting...")
	}
	if m.done {
		return m.summaryView()
	}
	if !m.initialized {
		return m.uninitializedView()
	}
	return m.progressView()
}

func (m buildModel) progressView() string {
	var b strings.Builder
	active := m.activeItems()

	if m.verbose {
		b.WriteString(m.verboseList(active))
	} else {
		b.WriteString(m.fallbackView(m.list.view(m.spinner.View(), active, listHeight(m.h, m.maxLines))))
	}

	builds := m.fmtBuilds()
	downloads := m.fmtDownloads()

	fmt.Fprintf(&b, "%s | %s\n",
		summaryHeader("Builds:", builds),
		summaryHeader("Downloads:", downloads),
	)
	fmt.Fprintf(&b, "%s | %s\n", builds, downloads)

	return b.String()
}

func (m buildModel) summaryView() string {
	var b strings.Builder
	builds := m.fmtBuilds()
	downloads := m.fmtDownloads()
	fmt.Fprintf(&b, "%s | %s\n",
		summaryHeader("Builds:", builds),
		summaryHeader("Downloads:", downloads),
	)
	fmt.Fprintf(&b, "%s | %s\n", builds, downloads)
	return b.String()
}

func (m buildModel) fmtBuilds() string {
	return summaryRow([]summaryCell{
		{"▶", m.buildProgress.running, "11"},
		{"✓", m.buildProgress.done, "10"},
		{"⧗", m.buildProgress.expected - m.buildProgress.done - m.buildProgress.running, "12"},
	}, "")
}

func (m buildModel) fmtDownloads() string {
	rTotal, unit := util.ConvertBytes(m.realiseProgress.expected)
	rDone := util.ConvertBytesToUnit(m.realiseProgress.done+m.downloads.done(), unit)
	suffix := fmt.Sprintf("[%.2f/%.2f %s]", rDone, rTotal, unit)
	return summaryRow([]summaryCell{
		{"↓", m.copyPathsProgress.running, "11"},
		{"✓", m.copyPathsProgress.done, "10"},
		{"⧗", m.copyPathsProgress.expected - m.copyPathsProgress.done - m.copyPathsProgress.running, "12"},
	}, suffix)
}
