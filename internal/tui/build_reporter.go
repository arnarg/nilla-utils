package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/util"
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

	buildProgress     progress
	copyPathsProgress progress
	realiseProgress   transfer

	downloads copies
	transfers map[int64]int64
	builds    map[int64]*build

	lastMsg string

	errs []error

	// Featured item tracking (pure: times come from Msgs only)
	featuredID      int64
	featuredKind    string // "build" or "download"
	featuredSince   time.Time
	rotationPending bool
}

func initBuildModel(verbose bool) buildModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return buildModel{
		verbose:           verbose,
		spinner:           s,
		buildProgress:     progress{id: 0},
		copyPathsProgress: progress{id: 0},
		realiseProgress:   transfer{id: 0},
		downloads:         map[int64]*copy{},
		builds:            map[int64]*build{},
		transfers:         map[int64]int64{},
		lastMsg:           "Initializing build...",
		errs:              []error{},
		featuredID:        0,
		rotationPending:   false,
	}
}

func (m buildModel) error() error {
	if len(m.errs) > 0 {
		return errors.Join(m.errs...)
	}
	return nil
}

func (m buildModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		scheduleRotationCheck(),
	)
}

func (m buildModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case rotationCheckMsg:
		now := time.Time(msg)
		m.rotationPending = false
		var cmd tea.Cmd

		// Check if we should rotate (minimum hold time passed)
		if m.featuredID != 0 && now.Sub(m.featuredSince) >= minFeatureDuration {
			if nextID, nextKind, ok := m.selectNextActive(); ok {
				// Rotate to next item
				m.featuredID = nextID
				m.featuredKind = nextKind
				m.featuredSince = now // Use the time from the tick message
				m.lastMsg = m.formatFeaturedItem()
			} else if !m.isFeaturedActive() {
				// Current item gone and no replacement
				m.featuredID = 0
				if !m.verbose {
					m.lastMsg = ""
				}
			}
		}

		// Reschedule check if we have active work
		if len(m.builds) > 0 || len(m.downloads) > 0 {
			m.rotationPending = true
			cmd = scheduleRotationCheck()
		}

		return m, cmd

	case featureMsg:
		// New item featured (either from start event or immediate rotation)
		m.featuredID = msg.id
		m.featuredKind = msg.kind
		m.featuredSince = msg.at // Time captured by the command
		m.lastMsg = m.formatFeaturedItem()
		return m, nil

	case nix.Event:
		return m.handleEvent(msg)
	}

	return m, nil
}

// selectNextActive is pure: it selects the next item without side effects or time calls
func (m buildModel) selectNextActive() (id int64, kind string, ok bool) {
	currentID := m.featuredID
	currentKind := m.featuredKind

	// Round-robin: alternate between categories when possible
	if currentKind == "download" {
		// Prefer switching to a build
		for id := range m.builds {
			return id, "build", true
		}
		// Or another download
		for id := range m.downloads {
			if id != currentID {
				return id, "download", true
			}
		}
	} else {
		// Prefer switching to a download
		for id := range m.downloads {
			return id, "download", true
		}
		// Or another build
		for id := range m.builds {
			if id != currentID {
				return id, "build", true
			}
		}
	}

	// If current item still exists, keep it (no rotation needed)
	if currentKind == "build" {
		if _, exists := m.builds[currentID]; exists {
			return currentID, "build", true
		}
	} else {
		if _, exists := m.downloads[currentID]; exists {
			return currentID, "download", true
		}
	}

	return 0, "", false
}

func (m buildModel) isFeaturedActive() bool {
	if m.featuredKind == "build" {
		_, ok := m.builds[m.featuredID]
		return ok
	}
	_, ok := m.downloads[m.featuredID]
	return ok
}

func (m buildModel) formatFeaturedItem() string {
	if m.featuredKind == "build" {
		if b, ok := m.builds[m.featuredID]; ok {
			return b.String()
		}
	} else {
		if d, ok := m.downloads[m.featuredID]; ok {
			return d.String()
		}
	}
	return ""
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
		if !m.initialized {
			m.initialized = true
		}
		return m, nil

	case nix.StartBuildsEvent:
		m.buildProgress = progress{id: ev.ID}
		if !m.initialized {
			m.initialized = true
		}
		return m, nil

	case nix.StartRealiseEvent:
		m.realiseProgress = transfer{id: ev.ID}
		return m, nil

	case nix.StartCopyPathEvent:
		m.downloads[ev.ID] = &copy{name: extractName(ev.Path)}

		// Feature this item immediately if nothing else is featured (via command for purity)
		if m.featuredID == 0 {
			return m, featureCmd(ev.ID, "download")
		}

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

		// Feature this item immediately if nothing else is featured (via command for purity)
		if m.featuredID == 0 {
			return m, featureCmd(ev.ID, "build")
		}
		return m, nil
	}

	return m, nil
}

func (m buildModel) handleStopEvent(ev nix.StopEvent) (tea.Model, tea.Cmd) {
	// Remove from tracking maps
	delete(m.builds, ev.ID)
	delete(m.transfers, ev.ID)

	if d, ok := m.downloads[ev.ID]; ok {
		m.realiseProgress.done += d.total
		delete(m.downloads, ev.ID)
	}

	// If featured item stopped, rotate immediately (via command to capture time)
	if m.featuredID == ev.ID {
		if nextID, nextKind, ok := m.selectNextActive(); ok {
			return m, featureCmd(nextID, nextKind)
		}
		// Nothing left to feature
		m.featuredID = 0
		if !m.verbose {
			m.lastMsg = ""
		}
	}

	// Clear message if all done
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
			return m, nil
		}

		b.phase = ev.Phase

		// Update display immediately if this is the featured build
		if m.featuredID == ev.ID && m.featuredKind == "build" {
			m.lastMsg = b.String()
		}
		return m, nil

	case nix.ResultProgressEvent:
		// Aggregate progress updates
		if ev.ID == m.copyPathsProgress.id {
			m.copyPathsProgress = progress{
				id:       ev.ID,
				done:     int(ev.Done),
				expected: int(ev.Expected),
				running:  ev.Running,
			}
			return m, nil
		} else if ev.ID == m.buildProgress.id {
			m.buildProgress = progress{
				id:       ev.ID,
				done:     int(ev.Done),
				expected: int(ev.Expected),
				running:  ev.Running,
			}
			return m, nil
		}

		// File transfer progress
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

		// Update display immediately if this is the featured download
		if m.featuredID == parent && m.featuredKind == "download" {
			m.lastMsg = d.String()
		}
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
		m.errs = append(m.errs, errors.New(ev.Text))
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
	if len(m.errs) > 0 {
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
		// Show featured item with spinner
		if m.featuredID != 0 {
			if display := m.formatFeaturedItem(); display != "" {
				fmt.Fprintf(strb, "%s%s\n", m.spinner.View(), display)
			}
		} else if m.lastMsg != "" {
			fmt.Fprintf(strb, "%s%s\n", m.spinner.View(), m.lastMsg)
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

	fmt.Fprintf(strb, "%s | %s\n", bhdr, dhdr)
	fmt.Fprintf(strb, "%s | %s\n", builds, downloads)

	return strb.String()
}

func fmtBuilds(m buildModel) string {
	running := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		SetString(fmt.Sprintf("▶ %d", m.buildProgress.running)).
		String()

	done := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		SetString(fmt.Sprintf("✓ %d", m.buildProgress.done)).
		String()

	remaining := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		SetString(fmt.Sprintf("⧗ %d", m.buildProgress.expected-m.buildProgress.done-m.buildProgress.running)).
		String()

	return fmt.Sprintf("%s | %s | %s", running, done, remaining)
}

func fmtDownloads(m buildModel) string {
	running := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		SetString(fmt.Sprintf("↓ %d", m.copyPathsProgress.running)).
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

	rTotal, unit := util.ConvertBytes(m.realiseProgress.expected)
	rDone := util.ConvertBytesToUnit(m.realiseProgress.done+m.downloads.done(), unit)
	rProgress := fmt.Sprintf("[%.2f/%.2f %s]", rDone, rTotal, unit)

	return fmt.Sprintf("%s | %s | %s %s", running, done, remaining, rProgress)
}
