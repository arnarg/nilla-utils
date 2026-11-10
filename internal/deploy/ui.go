package deploy

import (
	"fmt"
	"os"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/gen2brain/beeep"
	"golang.org/x/term"
)

// stage is a step of Session.Run before activation.
type stage = tui.StageID

const (
	stageEval stage = iota
	stageCopyDrv
	stageBuild
	stageDiff
	stageConfirm
	stageCopy
)

const confirmPrompt = "Do you want to continue?"

func stageTitle(st stage) string {
	switch st {
	case stageEval:
		return "Getting derivation path"
	case stageCopyDrv:
		return "Copying derivation to remote host"
	case stageBuild:
		return "Building system"
	case stageDiff:
		return "Comparing changes"
	case stageConfirm:
		return "Confirming changes"
	case stageCopy:
		return "Copying system to target"
	}
	return ""
}

// plannedStages returns the stages that Session.Run goes through for p.
func plannedStages(p *Plan) []stage {
	var stages []stage
	if p.BuildTarget != "" && p.StoreAddr != "" {
		stages = append(stages, stageEval, stageCopyDrv)
	}
	stages = append(stages, stageBuild)
	if !p.NoDiff {
		stages = append(stages, stageDiff)
	}
	if p.SubCmd == Build {
		return stages
	}
	if !p.Confirm {
		stages = append(stages, stageConfirm)
	}
	if !resolveCopy(p, "").skip {
		stages = append(stages, stageCopy)
	}
	return stages
}

// ui shows the progress of Session.Run.
type ui interface {
	begin(st stage)
	done(st stage, summary string)
	skip(st stage, reason string)
	fail(st stage)
	// reporter returns the progress reporter for a nix command run as
	// part of st, or nil to let nix write to the terminal.
	reporter(st stage) nix.ProgressReporter
	// showDiff shows the result of comparing generations.
	showDiff(r diff.Report)
	// confirm asks whether to continue, after showDiff if there was a diff.
	confirm() (bool, error)
	// close releases the terminal, before activation.
	close() error
}

// useTUI reports whether progress can be shown as one interactive
// program: it needs a terminal to draw on and to read answers from.
func useTUI(p *Plan) bool {
	return !p.Raw &&
		term.IsTerminal(int(os.Stderr.Fd())) &&
		term.IsTerminal(int(os.Stdin.Fd()))
}

func notifyAwaitingConfirmation(p *Plan) {
	beeep.AppName = "nilla-utils"
	_ = beeep.Notify("nilla-utils", fmt.Sprintf("%s '%s' ready, awaiting confirmation", p.SubCmd, p.Name), "")
}

// plainUI prints a section header per stage and runs a separate progress
// reporter per nix command, for --raw and when not on a terminal.
type plainUI struct {
	plan *Plan
}

func (u *plainUI) begin(st stage) {
	switch st {
	case stageConfirm:
		return
	case stageDiff, stageCopy:
		fmt.Fprintln(os.Stderr)
	}
	printSection(stageTitle(st))
}

func (u *plainUI) done(stage, string) {}
func (u *plainUI) skip(stage, string) {}
func (u *plainUI) fail(stage)         {}

func (u *plainUI) reporter(st stage) nix.ProgressReporter {
	if u.plan.Raw {
		return nil
	}
	mode := tui.ResolveReporterMode(u.plan.Compact, u.plan.Verbose)
	switch st {
	case stageBuild:
		return tui.NewBuildReporter(mode)
	case stageCopy:
		return tui.NewCopyReporter(mode)
	}
	return nil
}

func (u *plainUI) showDiff(r diff.Report) {
	if err := diff.NewTerminalRenderer().Render(os.Stderr, r); err != nil {
		log.Warnf("Failed to render diff: %v", err)
	}
}

func (u *plainUI) confirm() (bool, error) {
	if u.plan.Notify && !u.plan.Confirm {
		notifyAwaitingConfirmation(u.plan)
	}
	return Confirm(u.plan.Confirm)
}

func (u *plainUI) close() error { return nil }

// tuiUI shows all stages in a single tui.Pipeline.
type tuiUI struct {
	plan     *Plan
	pipeline *tui.Pipeline
	// confirming is set when the diff is shown at the confirmation.
	confirming bool
	report     *diff.Report
}

func newTUI(p *Plan, interrupt func()) *tuiUI {
	stages := plannedStages(p)
	u := &tuiUI{plan: p}

	defs := make([]tui.Stage, len(stages))
	for i, st := range stages {
		defs[i] = tui.Stage{ID: st, Title: stageTitle(st)}
		if st == stageConfirm {
			u.confirming = true
		}
	}

	opts := tui.PipelineOptions{
		Mode:        tui.ResolveReporterMode(p.Compact, p.Verbose),
		OnInterrupt: interrupt,
	}
	if p.Notify {
		opts.Notify = func() { notifyAwaitingConfirmation(p) }
	}

	u.pipeline = tui.NewPipeline(defs, opts)
	u.pipeline.Start()
	// Log lines would otherwise garble the program's output.
	log.SetOutput(u.pipeline.LogWriter())
	return u
}

func (u *tuiUI) begin(st stage) {
	if st != stageConfirm {
		u.pipeline.Begin(st)
	}
}
func (u *tuiUI) done(st stage, summary string) { u.pipeline.Done(st, summary) }
func (u *tuiUI) skip(st stage, reason string)  { u.pipeline.Skip(st, reason) }
func (u *tuiUI) fail(st stage)                 { u.pipeline.Fail(st) }

func (u *tuiUI) reporter(st stage) nix.ProgressReporter {
	switch st {
	case stageEval:
		return u.pipeline.Reporter(st, tui.BuildProgress, "Evaluating...")
	case stageBuild:
		return u.pipeline.Reporter(st, tui.BuildProgress, "Initializing build...")
	case stageCopyDrv, stageCopy:
		return u.pipeline.Reporter(st, tui.CopyProgress, "Initializing...")
	}
	return nil
}

func (u *tuiUI) showDiff(r diff.Report) {
	if u.confirming {
		u.report = &r
		return
	}
	u.pipeline.Print(diff.Render(r))
}

func (u *tuiUI) confirm() (bool, error) {
	return u.pipeline.Confirm(stageConfirm, confirmPrompt, u.report)
}

func (u *tuiUI) close() error {
	err := u.pipeline.Stop()
	log.SetOutput(os.Stderr)
	return err
}
