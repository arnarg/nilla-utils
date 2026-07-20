package deploy

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/gen2brain/beeep"
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

	args := []string{"--to", fmt.Sprintf("ssh://%s", p.DeployTarget)}

	if p.BuildTarget != "" && p.BuildTarget != p.DeployTarget {
		user, hostname := util.ParseTarget(p.BuildTarget)
		args = append(args, "--from", util.BuildStoreAddress(user, hostname))
	}

	args = append(args, outPath)

	return copyPlan{args: args}
}

func (s *Session) Build(ctx context.Context) (string, error) {
	p := s.Plan

	if p.BuildTarget != "" && p.StoreAddr != "" {
		if err := s.prepareRemoteBuild(ctx); err != nil {
			return "", err
		}
	}

	nargs := buildArgs(p)

	log.Debugf("Nix build arguments: %v", nargs)
	printSection("Building system")

	cmd := nix.Command("build").Args(nargs).Executor(s.BuildExecutor())
	if !p.Raw {
		cmd = cmd.Reporter(tui.NewBuildReporter(tui.ResolveReporterMode(p.Compact, p.Verbose)))
	}

	out, err := cmd.Run(ctx)
	if err != nil {
		log.Debugf("Nix build command failed with error: %v", err)
		return "", fmt.Errorf("failed to build configuration: %w", err)
	}
	log.Debugf("Build completed successfully, output path: %s", string(out))

	return string(out), nil
}

func (s *Session) prepareRemoteBuild(ctx context.Context) error {
	p := s.Plan

	printSection("Getting derivation path")
	derivationAttr := fmt.Sprintf("%s.drvPath", p.Attr)
	evalOut, err := nix.Command("eval").
		Args([]string{"-f", p.Source.FullNillaPath(), derivationAttr, "--raw"}).
		Executor(s.local).
		Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to get derivation path: %w", err)
	}

	printSection("Copying derivation to remote host")
	drvPath := strings.TrimSpace(string(evalOut))
	_, err = nix.Command("copy").
		Args([]string{"--to", p.StoreAddr, "--derivation", "-s", drvPath}).
		Executor(exec.NewAskpassExec(s.askpassSrv.SocketPath(), s.askpassSrv.Token(), "copy-derivation")).
		Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to copy derivation to remote: %w", err)
	}

	return nil
}

func (s *Session) Diff(ctx context.Context, outPath string) error {
	fmt.Fprintln(os.Stderr)
	printSection("Comparing changes")

	current, err := s.System.CurrentGeneration(s.target, s.Plan.Name)
	if err != nil {
		return fmt.Errorf("failed to resolve current generation: %w", err)
	}

	log.Debugf("Running diff: current=%s, new=%s", current.Path, outPath)

	if err := diff.Execute(
		&diff.Generation{Path: current.Path, Querier: current.Querier},
		&diff.Generation{Path: outPath, Querier: diff.NewExecutorQuerier(s.forDiff)},
	); err != nil {
		log.Debugf("Diff execution failed with error: %v", err)
		return fmt.Errorf("failed to compare changes: %w", err)
	}
	log.Debugf("Diff execution completed successfully")

	return nil
}

func Confirm(skip bool) (bool, error) {
	if skip {
		return true, nil
	}

	return tui.RunConfirm("Do you want to continue?")
}

func (s *Session) Copy(ctx context.Context, outPath string) error {
	cp := resolveCopy(s.Plan, outPath)
	if cp.skip {
		return nil
	}

	fmt.Fprintln(os.Stderr)
	printSection("Copying system to target")

	cmd := nix.Command("copy").
		Args(cp.args).
		Executor(s.CopyExecutor())
	if !s.Plan.Raw {
		cmd = cmd.Reporter(tui.NewCopyReporter(tui.ResolveReporterMode(s.Plan.Compact, s.Plan.Verbose)))
	}
	_, err := cmd.Run(ctx)
	return err
}

func (s *Session) Activate(ctx context.Context, outPath string) error {
	return s.System.Activate(ctx, s.target, outPath, s.Plan.SubCmd)
}

func (s *Session) Run(ctx context.Context) error {
	outPath, err := s.Build(ctx)
	if err != nil {
		return err
	}

	if err := s.Diff(ctx, outPath); err != nil {
		return err
	}

	if s.Plan.SubCmd == Build {
		return nil
	}

	if s.Plan.Notify && !s.Plan.Confirm {
		beeep.AppName = "nilla-utils"
		_ = beeep.Notify("nilla-utils", fmt.Sprintf("%s '%s' ready, awaiting confirmation", s.Plan.SubCmd, s.Plan.Name), "")
	}

	ok, err := Confirm(s.Plan.Confirm)
	if err != nil || !ok {
		return err
	}

	if err := s.Copy(ctx, outPath); err != nil {
		return err
	}

	return s.Activate(ctx, outPath)
}
