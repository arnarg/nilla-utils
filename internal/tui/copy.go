package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/util"
)

type CopyReporter struct {
	maxLines int
	verbose  bool
}

func NewCopyReporter(mode ReporterMode) *CopyReporter {
	reporter := &CopyReporter{
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

func (r *CopyReporter) Run(ctx context.Context, decoder *nix.ProgressDecoder) error {
	m := copyModel{
		baseModel:         newBaseModel(r.maxLines, r.verbose, "Initializing..."),
		copyPathsProgress: progress{id: 0},
		transferProgress:  transfer{id: 0},
		copies:            map[int64]*copy{},
		transfers:         map[int64]int64{},
	}
	return runTUIModel(ctx, m, decoder)
}

type copyModel struct {
	baseModel

	copyPathsProgress progress
	transferProgress  transfer

	copies    copies
	transfers map[int64]int64
}

func (m copyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.updateCommon(msg, len(m.copies)); handled {
		return m, cmd
	}
	if ev, ok := msg.(nix.Event); ok {
		return m.handleEvent(ev)
	}
	return m, nil
}

func (m copyModel) activeItems() []item {
	items := make([]item, 0, len(m.copies))
	for _, c := range m.copies {
		items = append(items, c)
	}
	slices.SortFunc(items, func(a, b item) int {
		return cmp.Compare(b.orderKey(), a.orderKey())
	})
	return items
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
		m.initialized = true
		return m, nil

	case nix.StartCopyPathEvent:
		m.copies[ev.ID] = &copy{id: ev.ID, name: ev.Path}
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
	if c, ok := m.copies[ev.ID]; ok {
		m.transferProgress.done += c.total
		m.list = m.list.add(c)
		delete(m.copies, ev.ID)

		if m.verbose {
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

	delete(m.transfers, ev.ID)

	if m.initialized && len(m.copies) < 1 {
		m.lastMsg = ""
	}

	return m, nil
}

func (m copyModel) handleResultEvent(ev nix.Event) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case nix.ResultProgressEvent:
		if ev.ID == m.copyPathsProgress.id {
			m.copyPathsProgress.done = int(ev.Done)
			m.copyPathsProgress.expected = int(ev.Expected)
			m.copyPathsProgress.running = ev.Running
			return m, nil
		}

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
	if ev.Level == nix.MsgLevelError {
		m.err = errors.New(ev.Text)
		return m, nil
	}

	if m.verbose {
		return m, tea.Printf("%s", ev.Text)
	}

	m.lastMsg = ev.Text
	return m, nil
}

func (m copyModel) View() tea.View {
	if m.err != nil {
		return tea.NewView(m.errorView("Copying failed! Exiting..."))
	}
	if m.done {
		return tea.NewView(m.summaryView())
	}
	if !m.initialized {
		return tea.NewView(m.uninitializedView())
	}
	return tea.NewView(m.progressView())
}

func (m copyModel) progressView() string {
	var b strings.Builder
	active := m.activeItems()

	if m.verbose {
		b.WriteString(m.verboseList(active))
	} else {
		b.WriteString(m.fallbackView(m.list.view(m.spinner.View(), active, listHeight(m.h, m.maxLines))))
	}

	transfers := m.fmtTransfers()

	fmt.Fprintf(&b, "%s\n", summaryHeader("Transfers:", transfers))
	fmt.Fprintf(&b, "%s\n", transfers)

	return b.String()
}

func (m copyModel) summaryView() string {
	var b strings.Builder
	transfers := m.fmtTransfers()
	fmt.Fprintf(&b, "%s\n", summaryHeader("Transfers:", transfers))
	fmt.Fprintf(&b, "%s\n", transfers)
	return b.String()
}

func (m copyModel) fmtTransfers() string {
	rTotal, unit := util.ConvertBytes(m.transferProgress.expected)
	rDone := util.ConvertBytesToUnit(m.transferProgress.done+m.copies.done(), unit)
	suffix := fmt.Sprintf("[%.2f/%.2f %s]", rDone, rTotal, unit)
	return summaryRow([]summaryCell{
		{"↑", m.copyPathsProgress.running, "11"},
		{"✓", m.copyPathsProgress.done, "10"},
		{"⧗", m.copyPathsProgress.expected - m.copyPathsProgress.done - m.copyPathsProgress.running, "12"},
	}, suffix)
}
