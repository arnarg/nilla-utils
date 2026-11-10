package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/nix"
)

func newTestPipelineModel(notify func()) *pipelineModel {
	m := &pipelineModel{
		maxLines: listHardMax,
		notify:   notify,
		stages: []*stageEntry{
			{Stage: Stage{ID: 1, Title: "Build"}},
			{Stage: Stage{ID: 2, Title: "Confirm"}},
			{Stage: Stage{ID: 3, Title: "Copy"}},
		},
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	return m
}

func view(m *pipelineModel) string {
	return stripANSI(m.View().Content)
}

// run executes cmd and the commands it batches, returning their messages.
// Ticks are not run, as they would block.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, run(c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func TestPipeline_StageFlow(t *testing.T) {
	m := newTestPipelineModel(nil)

	if v := view(m); !strings.Contains(v, "○ Build") || !strings.Contains(v, "○ Copy") {
		t.Fatalf("pending stages not shown:\n%s", v)
	}

	m.Update(stageBeginMsg{id: 1})
	bm := newBuildModel(listHardMax, false, "Initializing build...")
	bm.embedded = true
	m.Update(stageAttachMsg{id: 1, model: bm})
	m.Update(stageEventMsg{id: 1, ev: nix.StartBuildsEvent{ID: 7}})
	m.Update(stageEventMsg{id: 1, ev: nix.ResultProgressEvent{ID: 7, Done: 3, Expected: 3}})

	v := view(m)
	if strings.Contains(v, "○ Build") || !strings.Contains(v, "Build") {
		t.Fatalf("running stage not shown:\n%s", v)
	}
	if !strings.Contains(v, "Builds:") {
		t.Fatalf("build progress not shown:\n%s", v)
	}

	_, cmd := m.Update(stageEndMsg{id: 1, result: resultDone})
	if v := view(m); strings.Contains(v, "Build") {
		t.Fatalf("finished stage still live:\n%s", v)
	}
	if cmd == nil {
		t.Fatal("finishing a stage printed nothing")
	}
	if m.stages[0].state != stateFinished || m.stages[0].sub != nil {
		t.Fatal("stage not finished")
	}
}

func TestPipeline_StageErrorFromReporter(t *testing.T) {
	m := newTestPipelineModel(nil)
	m.Update(stageBeginMsg{id: 1})
	bm := newBuildModel(listHardMax, false, "")
	bm.embedded = true
	m.Update(stageAttachMsg{id: 1, model: bm})
	m.Update(stageEventMsg{id: 1, ev: nix.MessageEvent{Text: "boom", Level: nix.MsgLevelError}})

	reply := make(chan error, 1)
	m.Update(stageErrMsg{id: 1, reply: reply})
	if err := <-reply; err == nil || err.Error() != "boom" {
		t.Errorf("err = %v, want boom", err)
	}
}

func confirm(m *pipelineModel, report *diff.Report) (chan bool, tea.Cmd) {
	reply := make(chan bool, 1)
	_, cmd := m.Update(confirmMsg{id: 2, prompt: "Continue?", report: report, reply: reply})
	return reply, cmd
}

func TestPipeline_ConfirmWithBrowser(t *testing.T) {
	m := newTestPipelineModel(nil)
	r := testReport()
	reply, _ := confirm(m, &r)

	v := view(m)
	for _, want := range []string{"Continue? [y/N]", "All (4)", "alpha", "○ Copy"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}

	// Navigation keys go to the browser, not the prompt.
	m.Update(key("right"))
	select {
	case <-reply:
		t.Fatal("navigation answered the prompt")
	default:
	}

	m.Update(key("y"))
	if ok := <-reply; !ok {
		t.Error("y answered no")
	}
	if m.stages[1].state != stateFinished {
		t.Error("confirm stage not finished")
	}
}

func TestPipeline_ConfirmDefaultsToNo(t *testing.T) {
	for _, k := range []string{"n", "enter", "ctrl+c"} {
		m := newTestPipelineModel(nil)
		reply, _ := confirm(m, nil)
		m.Update(key(k))
		if ok := <-reply; ok {
			t.Errorf("%q answered yes", k)
		}
	}
}

func TestPipeline_InterruptTwiceQuits(t *testing.T) {
	interrupts := 0
	m := newTestPipelineModel(nil)
	m.onInterrupt = func() { interrupts++ }
	m.Update(stageBeginMsg{id: 1})

	m.Update(key("ctrl+c"))
	if interrupts != 1 || m.quitting {
		t.Fatalf("first ctrl+c: interrupts=%d quitting=%v", interrupts, m.quitting)
	}
	if !strings.Contains(view(m), "Interrupting") {
		t.Error("interrupt not shown")
	}

	_, cmd := m.Update(interruptMsg{})
	if interrupts != 1 || !m.quitting {
		t.Fatalf("second interrupt: interrupts=%d quitting=%v", interrupts, m.quitting)
	}
	if msgs := run(cmd); len(msgs) != 1 {
		t.Fatalf("second interrupt did not quit: %v", msgs)
	} else if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Fatalf("second interrupt did not quit: %T", msgs[0])
	}
}

func TestPipeline_NotifyFollowsFocus(t *testing.T) {
	tests := []struct {
		name   string
		before []tea.Msg
		after  []tea.Msg
		want   int
	}{
		{name: "no focus reports notifies", want: 1},
		{name: "focused does not notify", before: []tea.Msg{tea.FocusMsg{}}, want: 0},
		{name: "blurred notifies", before: []tea.Msg{tea.FocusMsg{}, tea.BlurMsg{}}, want: 1},
		{
			name:   "losing focus while waiting notifies once",
			before: []tea.Msg{tea.FocusMsg{}},
			after:  []tea.Msg{tea.BlurMsg{}, tea.FocusMsg{}, tea.BlurMsg{}},
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notified := 0
			m := newTestPipelineModel(func() { notified++ })
			for _, msg := range tt.before {
				m.Update(msg)
			}
			_, cmd := confirm(m, nil)
			run(cmd)
			for _, msg := range tt.after {
				_, cmd := m.Update(msg)
				run(cmd)
			}
			if notified != tt.want {
				t.Errorf("notified %d times, want %d", notified, tt.want)
			}
		})
	}
}

func TestPipeline_BlurOutsideConfirmDoesNotNotify(t *testing.T) {
	notified := 0
	m := newTestPipelineModel(func() { notified++ })
	m.Update(stageBeginMsg{id: 1})
	_, cmd := m.Update(tea.BlurMsg{})
	run(cmd)
	if notified != 0 {
		t.Errorf("notified %d times during build", notified)
	}
}

func TestPipeline_StopClearsView(t *testing.T) {
	m := newTestPipelineModel(nil)
	m.Update(stopMsg{})
	if v := view(m); v != "" {
		t.Errorf("view after stop = %q", v)
	}
	if !m.View().ReportFocus {
		t.Error("focus reporting off")
	}
}

func TestLineWriter(t *testing.T) {
	var lines []string
	w := &lineWriter{print: func(s string) { lines = append(lines, s) }}
	_, _ = w.Write([]byte("one\ntw"))
	_, _ = w.Write([]byte("o\n"))
	if strings.Join(lines, "|") != "one|two" {
		t.Errorf("lines = %q", lines)
	}
}

func TestPipeline_PrintsAfterConfirmKeepOrder(t *testing.T) {
	m := newTestPipelineModel(nil)
	r := testReport()
	reply, _ := confirm(m, &r)

	// Printing waits for the browser to be cleared from the view.
	_, cmd := m.Update(key("y"))
	<-reply
	if !m.holding || cmd == nil {
		t.Fatal("confirmation lines printed before the view shrank")
	}

	// Lines printed meanwhile come after them.
	m.Update(stageBeginMsg{id: 3})
	if cmd := m.finish(m.stages[2], resultDone, "copied"); cmd != nil {
		t.Fatal("finish printed ahead of the queue")
	}

	queue := stripANSI(strings.Join(m.printQueue, "\n"))
	iDiff := strings.Index(queue, "Version changes:")
	iConfirm := strings.Index(queue, "✓ Confirm")
	iCopy := strings.Index(queue, "✓ Copy")
	if iDiff < 0 || iDiff > iConfirm || iConfirm > iCopy {
		t.Fatalf("queue out of order:\n%s", queue)
	}

	// Chunks are printed one at a time until the queue is drained.
	_, cmd = m.Update(flushMsg{})
	for range 100 {
		if cmd == nil {
			break
		}
		_, cmd = m.Update(printedMsg{})
	}
	if m.printing || len(m.printQueue) != 0 {
		t.Fatal("queue not drained")
	}
}

func TestPipeline_StopWaitsForPrints(t *testing.T) {
	m := newTestPipelineModel(nil)
	m.Update(printMsg("one"))
	m.Update(printMsg("two"))
	if !m.printing || len(m.printQueue) != 1 {
		t.Fatalf("printing=%v queue=%v, want one in flight and one queued", m.printing, m.printQueue)
	}

	_, cmd := m.Update(stopMsg{})
	if cmd != nil {
		t.Fatal("stop did not wait for prints")
	}
	_, cmd = m.Update(printedMsg{})
	if cmd == nil || m.printQueue != nil && len(m.printQueue) != 0 {
		t.Fatal("second line not printed")
	}
	_, cmd = m.Update(printedMsg{})
	if msgs := run(cmd); len(msgs) != 1 {
		t.Fatalf("did not quit after printing: %v", msgs)
	} else if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Fatalf("did not quit after printing: %T", msgs[0])
	}
}
