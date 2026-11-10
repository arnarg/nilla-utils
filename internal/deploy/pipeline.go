package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
)

func printSection(text string) {
	fmt.Fprintf(os.Stderr, "\033[32m>\033[0m %s\n", text)
}

func buildArgs(p *Plan) []string {
	nargs := []string{"-f", p.Source.FullNillaPath(), p.Attr}

	switch {
	case p.SubCmd == Build && p.OutLink != "":
		nargs = append(nargs, "--out-link", p.OutLink)
	case p.SubCmd == Build && p.NoLink:
		nargs = append(nargs, "--no-link")
	case p.SubCmd != Build:
		nargs = append(nargs, "--no-link")
	}

	if p.BuildTarget != "" && p.StoreAddr != "" {
		nargs = append(nargs, "--store", p.StoreAddr, "--eval-store", "auto")
	}

	return nargs
}

type copyPlan struct {
	skip bool
	args []string
}

func resolveCopy(p *Plan, outPath string) copyPlan {
	if p.DeployTarget == "" {
		return copyPlan{skip: true}
	}

	if p.BuildTarget != "" && p.BuildTarget == p.DeployTarget {
		return copyPlan{skip: true}
	}

	args := []string{"--to", buildSSHURL(p.DeployTarget, p.DeployPort)}

	if p.BuildTarget != "" && p.BuildTarget != p.DeployTarget {
		user, hostname, _ := util.ParseTarget(p.BuildTarget)
		args = append(args, "--from", util.BuildStoreAddress(user, hostname, p.BuildPort))
	}

	args = append(args, outPath)

	return copyPlan{args: args}
}

func buildSSHURL(target, port string) string {
	user, hostname, _ := util.ParseTarget(target)
	return buildSSHURLFromParts(user, hostname, port)
}

// buildSSHURLFromParts builds an ssh:// store URL. Without an explicit user
// none is added, so ssh applies the User from ssh_config just as it did for
// nilla's own connection (whose control socket nix must find).
func buildSSHURLFromParts(user, hostname, port string) string {
	addr := "ssh://" + util.URLHost(hostname)
	if user != "" {
		addr = fmt.Sprintf("ssh://%s@%s", user, util.URLHost(hostname))
	}
	if port != "" {
		addr += "?port=" + port
	}
	return addr
}

func (s *Session) Build(ctx context.Context, u ui) (string, error) {
	p := s.Plan

	if p.BuildTarget != "" && p.StoreAddr != "" {
		if err := s.prepareRemoteBuild(ctx, u); err != nil {
			return "", err
		}
	}

	nargs := buildArgs(p)

	log.Debugf("Nix build arguments: %v", nargs)
	u.begin(stageBuild)

	cmd := nix.Command("build").Args(nargs).Executor(s.BuildExecutor())
	if r := u.reporter(stageBuild); r != nil {
		cmd = cmd.Reporter(r)
	}

	out, err := cmd.Run(ctx)
	if err != nil {
		u.fail(stageBuild)
		log.Debugf("Nix build command failed with error: %v", err)
		return "", fmt.Errorf("failed to build configuration: %w", err)
	}
	u.done(stageBuild, "")
	log.Debugf("Build completed successfully, output path: %s", string(out))

	return string(out), nil
}

func (s *Session) prepareRemoteBuild(ctx context.Context, u ui) error {
	p := s.Plan

	u.begin(stageEval)
	derivationAttr := fmt.Sprintf("%s.drvPath", p.Attr)
	cmd := nix.Command("eval").
		Args([]string{"-f", p.Source.FullNillaPath(), derivationAttr, "--raw"}).
		Executor(s.local)
	if r := u.reporter(stageEval); r != nil {
		cmd = cmd.Reporter(r)
	}
	evalOut, err := cmd.Run(ctx)
	if err != nil {
		u.fail(stageEval)
		return fmt.Errorf("failed to get derivation path: %w", err)
	}
	drvPath := strings.TrimSpace(string(evalOut))
	u.done(stageEval, extractStoreName(drvPath))

	u.begin(stageCopyDrv)
	cmd = nix.Command("copy").
		Args([]string{"--to", p.StoreAddr, "--derivation", "-s", drvPath}).
		Executor(s.nixRemoteExecutor())
	if r := u.reporter(stageCopyDrv); r != nil {
		cmd = cmd.Reporter(r)
	}
	if _, err := cmd.Run(ctx); err != nil {
		u.fail(stageCopyDrv)
		return fmt.Errorf("failed to copy derivation to remote: %w", err)
	}
	u.done(stageCopyDrv, "")

	return nil
}

// extractStoreName returns the name of a store path without its hash.
func extractStoreName(p string) string {
	base := p[strings.LastIndex(p, "/")+1:]
	if _, name, ok := strings.Cut(base, "-"); ok {
		return name
	}
	return base
}

func (s *Session) Diff(ctx context.Context, u ui, outPath string) error {
	if s.Plan.NoDiff {
		log.Debug("Skipping diff (--no-diff)")
		return nil
	}

	u.begin(stageDiff)

	current, err := s.System.CurrentGeneration(s.target, s.Plan.Name)
	if errors.Is(err, ErrNoCurrentGeneration) {
		log.Infof("No previous generation found, skipping diff")
		u.skip(stageDiff, "no previous generation")
		return nil
	}
	if err != nil {
		log.Warnf("Failed to resolve current generation, skipping diff: %v", err)
		u.skip(stageDiff, "current generation not found")
		return nil
	}

	log.Debugf("Running diff: current=%s, new=%s", current.Path, outPath)

	report, err := diff.CalculateReport(ctx,
		&diff.Generation{Path: current.Path, Querier: current.Querier},
		&diff.Generation{Path: outPath, Querier: diff.NewExecutorQuerier(s.forDiff)},
	)
	if err != nil {
		log.Warnf("Failed to compare changes, skipping diff: %v", err)
		u.skip(stageDiff, "comparison failed")
		return nil
	}
	log.Debugf("Diff execution completed successfully")

	u.done(stageDiff, tui.DiffSummary(report))
	u.showDiff(report)

	return nil
}

func Confirm(skip bool) (bool, error) {
	if skip {
		return true, nil
	}

	return tui.RunConfirm(confirmPrompt)
}

func (s *Session) confirm(u ui) (bool, error) {
	if s.Plan.Confirm {
		return true, nil
	}
	u.begin(stageConfirm)
	return u.confirm()
}

func (s *Session) Copy(ctx context.Context, u ui, outPath string) error {
	cp := resolveCopy(s.Plan, outPath)
	if cp.skip {
		return nil
	}

	u.begin(stageCopy)

	cmd := nix.Command("copy").
		Args(cp.args).
		Executor(s.CopyExecutor())
	if r := u.reporter(stageCopy); r != nil {
		cmd = cmd.Reporter(r)
	}
	if _, err := cmd.Run(ctx); err != nil {
		u.fail(stageCopy)
		return err
	}
	u.done(stageCopy, "")
	return nil
}

func (s *Session) Activate(ctx context.Context, outPath string) error {
	return s.System.Activate(ctx, s.target, outPath, s.Plan.SubCmd)
}

// errInterrupted is returned by Run when the user interrupts the stages.
var errInterrupted = errors.New("interrupted")

// Run builds, compares, confirms and copies the configuration as one
// interactive program, then activates it once the terminal is released.
func (s *Session) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var interrupted atomic.Bool
	var u ui = &plainUI{plan: s.Plan}
	if useTUI(s.Plan) {
		u = newTUI(s.Plan, func() {
			interrupted.Store(true)
			cancel()
		})
	}

	outPath, proceed, err := s.runStages(ctx, u)
	if cerr := u.close(); cerr != nil && err == nil {
		err = cerr
	}
	if interrupted.Load() {
		return errInterrupted
	}
	if err != nil {
		return err
	}

	// Progress and the diff go to stderr, so stdout holds only the path.
	if s.Plan.SubCmd == Build && s.Plan.PrintOutPaths {
		fmt.Println(outPath)
	}

	if !proceed {
		return nil
	}
	return s.Activate(ctx, outPath)
}

// runStages runs everything up to activation, and reports whether
// activation should follow.
func (s *Session) runStages(ctx context.Context, u ui) (string, bool, error) {
	outPath, err := s.Build(ctx, u)
	if err != nil {
		return "", false, err
	}

	if err := s.Diff(ctx, u, outPath); err != nil {
		return "", false, err
	}

	if s.Plan.SubCmd == Build {
		return outPath, false, nil
	}

	ok, err := s.confirm(u)
	if err != nil || !ok {
		return outPath, false, err
	}

	if err := s.Copy(ctx, u, outPath); err != nil {
		return "", false, err
	}

	return outPath, true, nil
}
