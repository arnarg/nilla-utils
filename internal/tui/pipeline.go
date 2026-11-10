package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/nix"
)

// StageID identifies a stage of a Pipeline.
type StageID int

// Stage is a step of a Pipeline, shown as pending until it begins.
type Stage struct {
	ID    StageID
	Title string
}

// ReporterKind selects how a stage renders nix progress.
type ReporterKind int

const (
	// BuildProgress shows builds and downloads, like nix build.
	BuildProgress ReporterKind = iota
	// CopyProgress shows path transfers, like nix copy.
	CopyProgress
)

type PipelineOptions struct {
	Mode ReporterMode
	// OnInterrupt is called on the first ctrl+c (or SIGINT/SIGTERM) and
	// should cancel the running work. A second one quits the program.
	OnInterrupt func()
	// Notify, if set, is called when a confirmation is awaited while the
	// terminal is not known to have focus, or loses focus while waiting.
	Notify func()
}

// Pipeline is a single bubbletea program that shows the progress of a
// sequence of stages. Finished stages collapse to a line in the
// scrollback; the running stage shows its progress below them, and the
// stages still to come are listed after it.
//
// Its methods are safe to call from any goroutine, also after the program
// has exited.
type Pipeline struct {
	prog   *tea.Program
	mode   ReporterMode
	exited chan struct{}
	runErr error

	stopOnce sync.Once
}

func NewPipeline(stages []Stage, opts PipelineOptions) *Pipeline {
	maxLines, verbose := opts.Mode.settings()
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	m := &pipelineModel{
		spinner:     s,
		maxLines:    maxLines,
		verbose:     verbose,
		notify:      opts.Notify,
		onInterrupt: opts.OnInterrupt,
	}
	for _, st := range stages {
		m.stages = append(m.stages, &stageEntry{Stage: st})
	}

	return &Pipeline{
		prog: tea.NewProgram(
			m,
			// Signals are turned into interruptMsg by Start.
			tea.WithoutSignalHandler(),
			// Output on stderr so that stdout only holds results.
			tea.WithOutput(os.Stderr),
			tea.WithFPS(30),
		),
		mode:   opts.Mode,
		exited: make(chan struct{}),
	}
}

// Start runs the program in the background.
func (p *Pipeline) Start() {
	go func() {
		defer close(p.exited)
		_, p.runErr = p.prog.Run()
	}()

	// Ctrl+c arrives as a key while the terminal is in raw mode, but
	// signals sent from elsewhere should interrupt just the same.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sig)
		for {
			select {
			case <-sig:
				p.prog.Send(interruptMsg{})
			case <-p.exited:
				return
			}
		}
	}()
}

// Stop drops the stages that never ran and waits for the program to exit.
func (p *Pipeline) Stop() error {
	p.stopOnce.Do(func() {
		p.prog.Send(stopMsg{})
	})
	<-p.exited
	return p.runErr
}

// Begin marks a stage as running.
func (p *Pipeline) Begin(id StageID) {
	p.prog.Send(stageBeginMsg{id: id})
}

// Done marks a stage as finished. An empty summary uses the summary of
// the stage's progress reporter, if it has one.
func (p *Pipeline) Done(id StageID, summary string) {
	p.prog.Send(stageEndMsg{id: id, result: resultDone, text: summary})
}

// Skip marks a stage as skipped.
func (p *Pipeline) Skip(id StageID, reason string) {
	p.prog.Send(stageEndMsg{id: id, result: resultSkipped, text: reason})
}

// Fail marks a stage as failed. The error itself is left for the caller
// to report.
func (p *Pipeline) Fail(id StageID) {
	p.prog.Send(stageEndMsg{id: id, result: resultFailed})
}

// Print prints text above the program.
func (p *Pipeline) Print(text string) {
	p.prog.Send(printMsg(text))
}

// Confirm shows prompt below an interactive browser of report (if not
// nil) as the given stage, and waits for an answer.
func (p *Pipeline) Confirm(id StageID, prompt string, report *diff.Report) (bool, error) {
	reply := make(chan bool, 1)
	p.prog.Send(confirmMsg{id: id, prompt: prompt, report: report, reply: reply})
	select {
	case ok := <-reply:
		return ok, nil
	case <-p.exited:
		return false, nil
	}
}

// Reporter returns a progress reporter that renders nix progress as part
// of a stage. initMsg is shown until nix reports progress.
func (p *Pipeline) Reporter(id StageID, kind ReporterKind, initMsg string) nix.ProgressReporter {
	return &pipelineReporter{p: p, id: id, kind: kind, initMsg: initMsg}
}

// LogWriter returns a writer that prints each line above the program, for
// output that would otherwise garble it.
func (p *Pipeline) LogWriter() io.Writer {
	return &lineWriter{print: p.Print}
}

type pipelineReporter struct {
	p       *Pipeline
	id      StageID
	kind    ReporterKind
	initMsg string
}

func (r *pipelineReporter) Run(ctx context.Context, decoder *nix.ProgressDecoder) error {
	maxLines, verbose := r.p.mode.settings()

	var m stageModel
	switch r.kind {
	case CopyProgress:
		cm := newCopyModel(maxLines, verbose, r.initMsg)
		cm.embedded = true
		m = cm
	default:
		bm := newBuildModel(maxLines, verbose, r.initMsg)
		bm.embedded = true
		m = bm
	}
	r.p.prog.Send(stageAttachMsg{id: r.id, model: m})

	for ev := range decoder.Events {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.p.prog.Send(stageEventMsg{id: r.id, ev: ev})
	}

	reply := make(chan error, 1)
	r.p.prog.Send(stageErrMsg{id: r.id, reply: reply})
	select {
	case err := <-reply:
		return err
	case <-r.p.exited:
		return nil
	}
}

// lineWriter sends complete lines to print.
type lineWriter struct {
	mu    sync.Mutex
	buf   []byte
	print func(string)
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.print(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	return len(b), nil
}

// stageModel is the progress view of a stage, fed with nix events.
type stageModel interface {
	tea.Model
	error() error
	summary() string
}

type stageState int

const (
	statePending stageState = iota
	stateRunning
	stateFinished
)

type stageResult int

const (
	resultDone stageResult = iota
	resultSkipped
	resultFailed
)

type stageEntry struct {
	Stage
	state   stageState
	started time.Time

	sub stageModel

	// Set while the stage asks for confirmation.
	confirm *confirmState
}

type confirmState struct {
	prompt  string
	browser *diffBrowser
	report  *diff.Report
	reply   chan bool
}

type (
	stageBeginMsg  struct{ id StageID }
	stageAttachMsg struct {
		id    StageID
		model stageModel
	}
	stageEventMsg struct {
		id StageID
		ev nix.Event
	}
	stageErrMsg struct {
		id    StageID
		reply chan error
	}
	stageEndMsg struct {
		id     StageID
		result stageResult
		text   string
	}
	confirmMsg struct {
		id     StageID
		prompt string
		report *diff.Report
		reply  chan bool
	}
	printMsg     string
	interruptMsg struct{}
	stopMsg      struct{}
	flushMsg     struct{}
	printedMsg   struct{}
	pipelineTick time.Time
)

// Printing above the view scrolls the whole screen, so whatever part of
// the view is scrolled off stays in the terminal's scrollback. To keep
// stale frames out of it, prints are kept shorter than the screen, and
// after the view shrinks they wait for it to be rendered.
const (
	// flushDelay is how long printing waits for the view to shrink.
	flushDelay = 100 * time.Millisecond
	// printChunk is the most lines printed at once when the terminal
	// height is unknown.
	printChunk = 10
)

func schedulePipelineTick() tea.Cmd {
	return tea.Tick(sweepInterval, func(t time.Time) tea.Msg {
		return pipelineTick(t)
	})
}

var (
	failStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	titleStyle   = lipgloss.NewStyle().Bold(true)
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

type pipelineModel struct {
	stages  []*stageEntry
	spinner spinner.Model

	maxLines int
	verbose  bool

	w, h int
	now  time.Time

	// The terminal's focus, once it has reported one.
	focusKnown bool
	focused    bool
	notified   bool
	notify     func()

	onInterrupt func()
	interrupted bool

	// Lines to print above the view, one chunk at a time so that they
	// stay in order: tea.Println commands run concurrently otherwise.
	printQueue []string
	printing   bool
	// holding is set while prints wait for the view to shrink.
	holding bool

	quitting bool
}

// print prints lines above the view, after any lines still queued.
func (m *pipelineModel) print(lines ...string) tea.Cmd {
	for _, l := range lines {
		m.printQueue = append(m.printQueue, strings.Split(l, "\n")...)
	}
	return m.pump()
}

// printAfterRender prints lines once the view has been rendered at its
// new size.
func (m *pipelineModel) printAfterRender(lines ...string) tea.Cmd {
	wasHolding := m.holding
	m.holding = true
	m.print(lines...)
	if wasHolding {
		return nil
	}
	return tea.Tick(flushDelay, func(time.Time) tea.Msg { return flushMsg{} })
}

// pump prints the next chunk of the queue, unless one is being printed.
// Once the queue is drained after a stop, it quits.
func (m *pipelineModel) pump() tea.Cmd {
	if m.printing || m.holding {
		return nil
	}
	if len(m.printQueue) == 0 {
		if m.quitting {
			return tea.Quit
		}
		return nil
	}

	n := printChunk
	if m.h > 0 {
		n = max(m.h/2, 1)
	}
	n = min(n, len(m.printQueue))
	chunk := strings.Join(m.printQueue[:n], "\n")
	m.printQueue = m.printQueue[n:]
	m.printing = true

	return tea.Sequence(tea.Println(chunk), func() tea.Msg { return printedMsg{} })
}

func (m *pipelineModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, schedulePipelineTick())
}

func (m *pipelineModel) stage(id StageID) *stageEntry {
	for _, s := range m.stages {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (m *pipelineModel) running() *stageEntry {
	for _, s := range m.stages {
		if s.state == stateRunning {
			return s
		}
	}
	return nil
}

func (m *pipelineModel) pendingCount() int {
	n := 0
	for _, s := range m.stages {
		if s.state == statePending {
			n++
		}
	}
	return n
}

// bodyHeight is the number of lines left for the running stage's body
// below its header and above the pending stages.
func (m *pipelineModel) bodyHeight() int {
	extra := 0
	if m.interrupted {
		extra = 1
	}
	return max(m.h-1-m.pendingCount()-extra, 1)
}

func (m *pipelineModel) resize(s *stageEntry) tea.Cmd {
	if s == nil || m.w == 0 {
		return nil
	}
	if s.confirm != nil && s.confirm.browser != nil {
		// Leave room for a blank line, the prompt, and a line of
		// margin so that the inline view never fills the screen.
		b := s.confirm.browser.setSize(m.w-2, m.bodyHeight()-3)
		s.confirm.browser = &b
	}
	if s.sub != nil {
		return m.updateSub(s, tea.WindowSizeMsg{Width: m.w - 2, Height: m.bodyHeight()})
	}
	return nil
}

func (m *pipelineModel) updateSub(s *stageEntry, msg tea.Msg) tea.Cmd {
	next, cmd := s.sub.Update(msg)
	s.sub = next.(stageModel)
	return cmd
}

func (m *pipelineModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, m.resize(m.running())

	case spinner.TickMsg:
		if msg.ID == m.spinner.ID() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		if s := m.running(); s != nil && s.sub != nil {
			return m, m.updateSub(s, msg)
		}
		return m, nil

	case pipelineTick:
		m.now = time.Time(msg)
		cmds := []tea.Cmd{schedulePipelineTick()}
		if s := m.running(); s != nil && s.sub != nil {
			cmds = append(cmds, m.updateSub(s, sweepMsg(msg)))
		}
		return m, tea.Batch(cmds...)

	case tea.FocusMsg:
		m.focusKnown, m.focused = true, true
		return m, nil

	case tea.BlurMsg:
		m.focusKnown, m.focused = true, false
		if s := m.running(); s != nil && s.confirm != nil {
			return m, m.maybeNotify()
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case interruptMsg:
		return m.interrupt()

	case stageBeginMsg:
		if s := m.stage(msg.id); s != nil {
			s.state = stateRunning
			s.started = time.Now()
		}
		return m, nil

	case stageAttachMsg:
		s := m.stage(msg.id)
		if s == nil {
			return m, nil
		}
		s.sub = msg.model
		return m, tea.Batch(s.sub.Init(), m.resize(s))

	case stageEventMsg:
		if s := m.stage(msg.id); s != nil && s.sub != nil {
			return m, m.updateSub(s, msg.ev)
		}
		return m, nil

	case stageErrMsg:
		var err error
		if s := m.stage(msg.id); s != nil && s.sub != nil {
			err = s.sub.error()
		}
		msg.reply <- err
		return m, nil

	case stageEndMsg:
		s := m.stage(msg.id)
		if s == nil {
			return m, nil
		}
		return m, m.finish(s, msg.result, msg.text)

	case confirmMsg:
		return m.startConfirm(msg)

	case printMsg:
		return m, m.print(string(msg))

	case flushMsg:
		m.holding = false
		return m, m.pump()

	case printedMsg:
		m.printing = false
		return m, m.pump()

	case stopMsg:
		// The view is cleared, and the program quits once all lines are
		// printed.
		m.quitting = true
		return m, m.pump()
	}

	return m, nil
}

func (m *pipelineModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.running()
	if s != nil && s.confirm != nil {
		c := s.confirm
		if c.browser != nil {
			b, consumed := c.browser.update(msg)
			c.browser = &b
			if consumed {
				return m, nil
			}
		}
		switch msg.String() {
		case "y", "Y":
			return m, m.answer(s, true)
		case "n", "N", "q", "enter", "ctrl+c":
			return m, m.answer(s, false)
		}
		return m, nil
	}

	if msg.String() == "ctrl+c" {
		return m.interrupt()
	}
	return m, nil
}

func (m *pipelineModel) interrupt() (tea.Model, tea.Cmd) {
	if s := m.running(); s != nil && s.confirm != nil {
		return m, m.answer(s, false)
	}
	if m.interrupted {
		m.quitting = true
		return m, tea.Quit
	}
	m.interrupted = true
	if m.onInterrupt != nil {
		m.onInterrupt()
	}
	return m, m.resize(m.running())
}

func (m *pipelineModel) startConfirm(msg confirmMsg) (tea.Model, tea.Cmd) {
	s := m.stage(msg.id)
	if s == nil {
		msg.reply <- false
		return m, nil
	}
	s.state = stateRunning
	s.started = time.Now()
	s.confirm = &confirmState{prompt: msg.prompt, report: msg.report, reply: msg.reply}
	if msg.report != nil {
		b := newDiffBrowser(*msg.report)
		s.confirm.browser = &b
	}
	return m, tea.Batch(m.resize(s), m.maybeNotify())
}

// maybeNotify notifies once that a confirmation is awaited, unless the
// terminal is known to have focus. Without focus reports it notifies, as
// there is no telling whether the user is looking.
func (m *pipelineModel) maybeNotify() tea.Cmd {
	if m.notify == nil || m.notified || (m.focusKnown && m.focused) {
		return nil
	}
	m.notified = true
	notify := m.notify
	return func() tea.Msg {
		notify()
		return nil
	}
}

func (m *pipelineModel) answer(s *stageEntry, ok bool) tea.Cmd {
	c := s.confirm
	s.confirm = nil
	c.reply <- ok

	// The browser may fill the screen, so print once it is gone.
	var lines []string
	if c.report != nil {
		// Keep the full diff in the scrollback.
		lines = append(lines, diff.Render(*c.report))
	}
	if ok {
		lines = append(lines, m.finishLine(s, resultDone, "continuing"))
	} else {
		lines = append(lines, m.finishLine(s, resultSkipped, "cancelled"))
	}
	return m.printAfterRender(lines...)
}

func (m *pipelineModel) finish(s *stageEntry, result stageResult, text string) tea.Cmd {
	if s.state == stateFinished {
		return nil
	}
	return m.print(m.finishLine(s, result, text))
}

// finishLine marks a stage as finished and returns its line for the
// scrollback.
func (m *pipelineModel) finishLine(s *stageEntry, result stageResult, text string) string {
	if s.state == statePending {
		s.started = time.Now()
	}
	s.state = stateFinished

	if text == "" && s.sub != nil && result == resultDone {
		text = s.sub.summary()
	}
	s.sub = nil

	var line string
	switch result {
	case resultDone:
		line = fmt.Sprintf("%s %s", checkStyle.Render("✓"), titleStyle.Render(s.Title))
		if text != "" {
			line += "  " + text
		}
		line += "  " + mutedStyle.Render(formatElapsed(time.Since(s.started)))
	case resultSkipped:
		line = mutedStyle.Render("– " + s.Title)
		if text != "" {
			line += "  " + mutedStyle.Render(text)
		}
	case resultFailed:
		line = fmt.Sprintf("%s %s", failStyle.Render("✗"), failStyle.Bold(true).Render(s.Title))
		line += "  " + mutedStyle.Render(formatElapsed(time.Since(s.started)))
	}

	return line
}

func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	d = d.Round(time.Second)
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return "  " + strings.ReplaceAll(s, "\n", "\n  ") + "\n"
}

func (m *pipelineModel) View() tea.View {
	v := tea.NewView("")
	v.ReportFocus = true
	if m.quitting {
		v.SetContent("")
		return v
	}

	now := m.now
	var b strings.Builder
	for _, s := range m.stages {
		switch s.state {
		case statePending:
			fmt.Fprintf(&b, "%s\n", mutedStyle.Render("○ "+s.Title))

		case stateRunning:
			elapsed := ""
			if !now.IsZero() && now.After(s.started) {
				elapsed = "  " + mutedStyle.Render(formatElapsed(now.Sub(s.started)))
			}
			fmt.Fprintf(&b, "%s%s%s\n", m.spinner.View(), titleStyle.Render(s.Title), elapsed)

			switch {
			case s.confirm != nil:
				if s.confirm.browser != nil {
					b.WriteString(indent(s.confirm.browser.view()))
					b.WriteString("\n")
				}
				fmt.Fprintf(&b, "  %s %s\n", s.confirm.prompt, mutedStyle.Render("[y/N]"))
			case s.sub != nil:
				b.WriteString(indent(s.sub.View().Content))
			}

			if m.interrupted {
				fmt.Fprintf(&b, "  %s\n", warningStyle.Render("Interrupting, press ctrl+c again to quit..."))
			}
		}
	}

	content := strings.TrimRight(b.String(), "\n")
	if m.w > 0 {
		content = lipgloss.NewStyle().MaxWidth(m.w).Render(content)
	}
	v.SetContent(content)
	return v
}
