package deploy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/tui"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/gen2brain/beeep"
)

// DiskMode selects how the target's disks are prepared before installing.
type DiskMode string

const (
	// DiskNone expects the root mount point to already be prepared.
	DiskNone DiskMode = "none"
	// DiskMount mounts existing disko-managed filesystems.
	DiskMount DiskMode = "mount"
	// DiskFormat creates disko-managed partitions and filesystems that do not
	// exist yet, then mounts them. Existing filesystems are kept.
	DiskFormat DiskMode = "format"
	// DiskDestroy wipes all disko-managed disks, recreates them and mounts
	// them.
	DiskDestroy DiskMode = "destroy"
)

var diskModes = []DiskMode{DiskNone, DiskMount, DiskFormat, DiskDestroy}

// ParseDiskMode parses the value of the --disk flag. An empty string means
// the flag was not given.
func ParseDiskMode(s string) (DiskMode, error) {
	if s == "" {
		return "", nil
	}
	m := DiskMode(s)
	if !slices.Contains(diskModes, m) {
		return "", fmt.Errorf("invalid disk mode %q, expected one of: none, mount, format, destroy", s)
	}
	return m, nil
}

// Wipes reports whether the mode writes partition tables or filesystems.
func (m DiskMode) Wipes() bool {
	return m == DiskFormat || m == DiskDestroy
}

// diskScriptAttrs returns the disko scripts under config.system.build to run,
// in order, for the given mode.
func diskScriptAttrs(m DiskMode) []string {
	switch m {
	case DiskDestroy:
		// diskoScript unmounts, destroys, formats and mounts.
		return []string{"diskoScript"}
	case DiskFormat:
		// formatScript only creates, it does not mount.
		return []string{"formatScript", "mountScript"}
	case DiskMount:
		return []string{"mountScript"}
	default:
		return nil
	}
}

type InstallOptions struct {
	Disk             DiskMode
	Root             string
	RootSet          bool
	ExtraFiles       string
	Substitute       bool
	NoReboot         bool
	NoInstallerCheck bool
}

// diskoInfo is what nilla-utils needs to know about a system's disko
// configuration. It is nil when the system does not use disko.
type diskoInfo struct {
	RootMountPoint string            `json:"rootMountPoint"`
	Disks          map[string]string `json:"disks"`
	// Swaps are the system's swap devices. disko's mount script does not
	// enable swap, so it is done separately.
	Swaps []string `json:"swaps"`
}

// installInfo is what the install evaluates from the system up front.
type installInfo struct {
	Disko *diskoInfo `json:"disko"`
	// Substituters and TrustedPublicKeys come from the system's nix.settings.
	// They are passed on when building on the target, since the installer
	// only knows about cache.nixos.org.
	Substituters      []string `json:"substituters"`
	TrustedPublicKeys []string `json:"trustedPublicKeys"`
}

const installInfoApply = `r:
  let c = r.config; in {
    disko =
      if c ? disko && c.system.build ? diskoScript then {
        rootMountPoint = c.disko.rootMountPoint;
        disks = builtins.mapAttrs (_: d: d.device) c.disko.devices.disk;
        swaps = map (s: s.device) (builtins.filter (s: !s.randomEncryption.enable) c.swapDevices);
      } else null;
    substituters = c.nix.settings.substituters or [ ];
    trustedPublicKeys = c.nix.settings.trusted-public-keys or [ ];
  }`

func (s *Session) evalInstallInfo(ctx context.Context) (*installInfo, error) {
	attr := fmt.Sprintf("systems.nixos.\"%s\".result", s.Plan.Name)
	out, err := nix.Command("eval").
		Args([]string{"-f", s.Plan.Source.FullNillaPath(), attr, "--json", "--apply", installInfoApply}).
		Executor(s.local).
		Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate system configuration: %w", err)
	}
	return parseInstallInfo(out)
}

func parseInstallInfo(out []byte) (*installInfo, error) {
	var info installInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("failed to parse system configuration: %w", err)
	}
	if info.Disko != nil && len(info.Disko.Disks) == 0 {
		info.Disko = nil
	}
	return &info, nil
}

// buildPlacement is where the system is built.
type buildPlacement int

const (
	buildLocal buildPlacement = iota
	buildOnBuilder
	buildOnTarget
)

func resolvePlacement(p *Plan) buildPlacement {
	switch {
	case p.BuildTarget == "":
		return buildLocal
	case p.BuildTarget == p.DeployTarget:
		return buildOnTarget
	default:
		return buildOnBuilder
	}
}

func (b buildPlacement) String() string {
	switch b {
	case buildOnBuilder:
		return "remote builder"
	case buildOnTarget:
		return "target"
	default:
		return "local"
	}
}

// remoteStoreURL is like installStoreURL but for the ssh-ng protocol, which
// remote builds use.
func remoteStoreURL(user, host, port, root string) string {
	return "ssh-ng" + strings.TrimPrefix(installStoreURL(user, host, port, root), "ssh")
}

// substituterArgs passes the system's binary caches on to a build on the
// target. These have to be given to nix running on the target: a daemon
// behind ssh-ng silently ignores substituters sent by the client.
func substituterArgs(info *installInfo) []string {
	var args []string
	if len(info.Substituters) > 0 {
		args = append(args, "--option", "extra-substituters", strings.Join(info.Substituters, " "))
	}
	if len(info.TrustedPublicKeys) > 0 {
		args = append(args, "--option", "extra-trusted-public-keys", strings.Join(info.TrustedPublicKeys, " "))
	}
	return args
}

// drvPathsApply evaluates the derivation paths of the named attributes of
// config.system.build in one go, so remote builds evaluate the system once.
func drvPathsApply(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return fmt.Sprintf("b: map (n: b.${n}.drvPath) [ %s ]", strings.Join(quoted, " "))
}

// outputInstallables turns derivation paths into installables for their
// default output.
func outputInstallables(drvs []string) []string {
	out := make([]string, len(drvs))
	for i, d := range drvs {
		out[i] = d + "^out"
	}
	return out
}

// resolveDiskMode picks the disk mode. Without disko nothing but "none" is
// possible. With disko the mode has to be chosen explicitly, since guessing
// wrong may wipe disks or install onto the wrong filesystems.
func resolveDiskMode(requested DiskMode, info *diskoInfo) (DiskMode, error) {
	if info == nil {
		if requested != "" && requested != DiskNone {
			return "", fmt.Errorf("--disk %s requires the system to use disko (no disko disks found in its configuration)", requested)
		}
		return DiskNone, nil
	}
	if requested == "" {
		return "", errors.New("system uses disko, choose how to prepare disks with --disk (none, mount, format or destroy)")
	}
	return requested, nil
}

// resolveRoot picks the mount point to install into. Disko mounts into its
// own rootMountPoint, so an explicit --root must agree with it.
func resolveRoot(opts InstallOptions, mode DiskMode, info *diskoInfo) (string, error) {
	root := opts.Root
	if root == "" {
		root = "/mnt"
	}
	if mode == DiskNone || info == nil || info.RootMountPoint == "" {
		return root, nil
	}
	if opts.RootSet && root != info.RootMountPoint {
		return "", fmt.Errorf("--root %s does not match disko.rootMountPoint %s", root, info.RootMountPoint)
	}
	return info.RootMountPoint, nil
}

// targetFacts is what the preflight check learns about the target.
type targetFacts struct {
	UID              string
	VariantID        string
	HasNixOSInstall  bool
	RootMounted      bool
	RootMountedKnown bool
}

const factsScript = `printf 'uid=%s\n' "$(id -u)"
if [ -r /etc/os-release ]; then (. /etc/os-release; printf 'variant=%s\n' "${VARIANT_ID:-}"); fi
if command -v nixos-install >/dev/null 2>&1; then echo nixos_install=1; fi
if mountpoint -q "$1" 2>/dev/null; then echo root_mounted=1; else echo root_mounted=0; fi`

func parseFacts(out string) targetFacts {
	var f targetFacts
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		switch k {
		case "uid":
			f.UID = v
		case "variant":
			f.VariantID = v
		case "nixos_install":
			f.HasNixOSInstall = v == "1"
		case "root_mounted":
			f.RootMounted = v == "1"
			f.RootMountedKnown = true
		}
	}
	return f
}

func validateFacts(f targetFacts, mode DiskMode, root string, opts InstallOptions) error {
	if f.UID != "0" {
		return errors.New("install must connect to the target as root (e.g. --target root@host)")
	}
	if !opts.NoInstallerCheck && f.VariantID != "installer" {
		return errors.New("target does not look like it is booted into a NixOS installer (VARIANT_ID in /etc/os-release is not \"installer\"), pass --no-installer-check if you are sure")
	}
	if !f.HasNixOSInstall {
		return errors.New("nixos-install was not found on the target")
	}
	if mode == DiskNone && f.RootMountedKnown && !f.RootMounted {
		return fmt.Errorf("%s is not a mount point on the target, mount the target filesystems first or use --disk", root)
	}
	return nil
}

// diskState is a disk declared in disko as seen on the target.
type diskState struct {
	Name    string
	Device  string
	Present bool
	Info    string
}

const diskCheckScript = `for d in "$@"; do
  if [ -b "$d" ]; then printf '1\t%s\t%s\n' "$d" "$(lsblk -dno SIZE,MODEL "$d" 2>/dev/null | head -n1)"
  else printf '0\t%s\t\n' "$d"; fi
done`

func parseDiskCheck(out string, disks map[string]string) []diskState {
	seen := map[string]diskState{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 3)
		if len(parts) < 2 {
			continue
		}
		st := diskState{Device: parts[1], Present: parts[0] == "1"}
		if len(parts) == 3 {
			st.Info = strings.Join(strings.Fields(parts[2]), " ")
		}
		seen[st.Device] = st
	}

	names := make([]string, 0, len(disks))
	for name := range disks {
		names = append(names, name)
	}
	slices.Sort(names)

	states := make([]diskState, 0, len(names))
	for _, name := range names {
		st := seen[disks[name]]
		st.Name = name
		st.Device = disks[name]
		states = append(states, st)
	}
	return states
}

// installStoreURL is the store URL used to copy into the target. With a
// non-empty root the closure is written straight into the store under root
// instead of the installer's (usually tmpfs backed) store, so nixos-install
// does not need to copy it a second time. An empty user is left out so ssh
// picks the user from its config, the same way the SSH executor does.
func installStoreURL(user, host, port, root string) string {
	addr := "ssh://" + host
	if user != "" {
		addr = "ssh://" + user + "@" + host
	}
	q := url.Values{}
	if port != "" {
		q.Set("port", port)
	}
	if root != "" {
		q.Set("remote-store", "local?root="+root)
	}
	if len(q) > 0 {
		addr += "?" + q.Encode()
	}
	return addr
}

func nixosInstallArgs(root, toplevel string) []string {
	return []string{
		"nixos-install",
		"--root", root,
		"--no-root-passwd",
		"--no-channel-copy",
		"--store-path", toplevel,
	}
}

// finalizeScript unmounts the target and schedules a reboot. Pools are
// exported because the installed system has a different hostId than the
// installer, and ZFS refuses to import a pool last used by another host.
const finalizeScript = `root=$1
sync
umount -R "$root" || echo "warning: failed to unmount $root" >&2
swapoff -a || true
if command -v zpool >/dev/null 2>&1; then zpool export -a || echo "warning: failed to export ZFS pools" >&2; fi
systemd-run --on-active=2 --no-block systemctl reboot`

// swaponScript enables swap devices that are not already active. Failing to
// enable swap only warns, the install works without it.
const swaponScript = `for d in "$@"; do
  if swapon --show=NAME --noheadings | grep -qxF "$(readlink -f "$d")"; then continue; fi
  swapon "$d" || echo "warning: failed to enable swap on $d" >&2
done`

func capture(ctx context.Context, ex exec.Executor, name string, args ...string) (string, error) {
	c, err := ex.CommandContext(ctx, name, args...)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	c.SetStdout(&out)
	c.SetStderr(os.Stderr)
	if err := c.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func stream(ctx context.Context, ex exec.Executor, stdin io.Reader, name string, args ...string) error {
	c, err := ex.CommandContext(ctx, name, args...)
	if err != nil {
		return err
	}
	if stdin != nil {
		c.SetStdin(stdin)
	}
	c.SetStdout(os.Stdout)
	c.SetStderr(os.Stderr)
	return c.Run()
}

// Install builds the system and installs it onto the target, which must be
// booted into a NixOS installer.
func (s *Session) Install(ctx context.Context, opts InstallOptions) error {
	p := s.Plan
	if p.DeployTarget == "" {
		return errors.New("install requires --target")
	}

	if opts.ExtraFiles != "" {
		st, err := os.Stat(opts.ExtraFiles)
		if err != nil {
			return fmt.Errorf("failed to read --extra-files: %w", err)
		}
		if !st.IsDir() {
			return fmt.Errorf("--extra-files %s is not a directory", opts.ExtraFiles)
		}
	}

	printSection("Evaluating system configuration")
	sysInfo, err := s.evalInstallInfo(ctx)
	if err != nil {
		return err
	}
	info := sysInfo.Disko
	mode, err := resolveDiskMode(opts.Disk, info)
	if err != nil {
		return err
	}
	root, err := resolveRoot(opts, mode, info)
	if err != nil {
		return err
	}
	placement := resolvePlacement(p)
	user, host, port := util.ParseTarget(p.DeployTarget)

	// Check the target before spending time on a build.
	printSection("Checking target")
	out, err := capture(ctx, s.target, "sh", "-c", factsScript, "sh", root)
	if err != nil {
		return fmt.Errorf("failed to inspect target: %w", err)
	}
	facts := parseFacts(out)
	log.Debugf("Target facts: %+v", facts)
	if err := validateFacts(facts, mode, root, opts); err != nil {
		return err
	}

	var disks []diskState
	if mode != DiskNone {
		devices := make([]string, 0, len(info.Disks))
		for _, d := range info.Disks {
			devices = append(devices, d)
		}
		slices.Sort(devices)
		out, err := capture(ctx, s.target, "sh", append([]string{"-c", diskCheckScript, "sh"}, devices...)...)
		if err != nil {
			return fmt.Errorf("failed to inspect target disks: %w", err)
		}
		disks = parseDiskCheck(out, info.Disks)
		for _, d := range disks {
			if !d.Present {
				return fmt.Errorf("disk %q (%s) does not exist on the target", d.Name, d.Device)
			}
		}
	}

	scriptAttrs := diskScriptAttrs(mode)

	// Remote builds evaluate all derivations they need once.
	var drvs map[string]string
	if placement != buildLocal {
		names := append([]string{"toplevel"}, scriptAttrs...)
		if drvs, err = s.evalDrvPaths(ctx, names); err != nil {
			return err
		}
	}

	// When building on the target, the system is built straight into the
	// store under root. That only exists after the disks are prepared, so
	// unless they already are, the system is built after confirming.
	buildLater := placement == buildOnTarget && mode != DiskNone

	var toplevel string
	var scripts []string
	switch placement {
	case buildLocal:
		if toplevel, scripts, err = s.buildInstall(ctx, mode); err != nil {
			return err
		}
	case buildOnBuilder:
		fmt.Fprintln(os.Stderr)
		printSection(fmt.Sprintf("Building system on %s", p.BuildTarget))
		paths, err := s.buildRemote(ctx, s.Plan.StoreAddr, pick(drvs, "toplevel", scriptAttrs...)...)
		if err != nil {
			return err
		}
		toplevel, scripts = paths[0], paths[1:]
	case buildOnTarget:
		if len(scriptAttrs) > 0 {
			fmt.Fprintln(os.Stderr)
			printSection("Building disk scripts on target")
			if scripts, err = s.buildOnTarget(ctx, user, host, port, "", substituterArgs(sysInfo), pick(drvs, "", scriptAttrs...)...); err != nil {
				return err
			}
		}
		if !buildLater {
			if toplevel, err = s.buildSystemOnTarget(ctx, sysInfo, drvs["toplevel"], user, host, port, root); err != nil {
				return err
			}
		}
	}

	s.printInstallSummary(mode, root, opts, disks, placement, buildLater)

	if p.Notify && !p.Confirm {
		beeep.AppName = "nilla-utils"
		_ = beeep.Notify("nilla-utils", fmt.Sprintf("Install '%s' ready, awaiting confirmation", p.Name), "")
	}
	ok, err := confirmInstall(p.Confirm, p.Name, mode)
	if err != nil || !ok {
		return err
	}

	if len(scripts) > 0 {
		if placement != buildOnTarget {
			fmt.Fprintln(os.Stderr)
			printSection("Copying disk scripts to target")
			if err := s.copyTo(ctx, installStoreURL(user, host, port, ""), opts.Substitute, scripts...); err != nil {
				return fmt.Errorf("failed to copy disk scripts: %w", err)
			}
		}

		fmt.Fprintln(os.Stderr)
		printSection(fmt.Sprintf("Preparing disks (%s)", mode))
		for _, script := range scripts {
			if err := stream(ctx, s.target, nil, script); err != nil {
				return fmt.Errorf("disk script %s failed: %w", script, err)
			}
		}

		out, err := capture(ctx, s.target, "sh", "-c", factsScript, "sh", root)
		if err != nil {
			return fmt.Errorf("failed to inspect target: %w", err)
		}
		if f := parseFacts(out); f.RootMountedKnown && !f.RootMounted {
			return fmt.Errorf("%s is not mounted after preparing disks", root)
		}

		if len(info.Swaps) > 0 {
			if err := stream(ctx, s.target, nil, "sh", append([]string{"-c", swaponScript, "sh"}, info.Swaps...)...); err != nil {
				log.Warnf("Failed to enable swap: %v", err)
			}
		}
	}

	if buildLater {
		if toplevel, err = s.buildSystemOnTarget(ctx, sysInfo, drvs["toplevel"], user, host, port, root); err != nil {
			return err
		}
	}

	if placement != buildOnTarget {
		fmt.Fprintln(os.Stderr)
		printSection(fmt.Sprintf("Copying system to %s on target", root))
		if err := s.copyTo(ctx, installStoreURL(user, host, port, root), opts.Substitute, toplevel); err != nil {
			return fmt.Errorf("failed to copy system: %w", err)
		}
	}

	if opts.ExtraFiles != "" {
		fmt.Fprintln(os.Stderr)
		printSection("Copying extra files")
		if err := s.copyExtraFiles(ctx, opts.ExtraFiles, root); err != nil {
			return fmt.Errorf("failed to copy extra files: %w", err)
		}
	}

	fmt.Fprintln(os.Stderr)
	printSection("Installing system")
	args := nixosInstallArgs(root, toplevel)
	if err := stream(ctx, s.target, nil, args[0], args[1:]...); err != nil {
		return fmt.Errorf("nixos-install failed: %w", err)
	}

	fmt.Fprintln(os.Stderr)
	if opts.NoReboot {
		log.Infof("Installation complete, %s is still mounted on the target", root)
		return nil
	}

	printSection("Unmounting and rebooting")
	if err := stream(ctx, s.target, nil, "sh", "-c", finalizeScript, "sh", root); err != nil {
		return fmt.Errorf("failed to reboot target: %w", err)
	}
	log.Infof("Installation complete, target is rebooting")
	log.Infof("Its SSH host key will change unless one was provided with --extra-files")

	return nil
}

func (s *Session) buildInstall(ctx context.Context, mode DiskMode) (string, []string, error) {
	p := s.Plan
	attrs := []string{p.Attr}
	for _, a := range diskScriptAttrs(mode) {
		attrs = append(attrs, fmt.Sprintf("systems.nixos.\"%s\".result.config.system.build.%s", p.Name, a))
	}

	fmt.Fprintln(os.Stderr)
	printSection("Building system")
	args := append([]string{"-f", p.Source.FullNillaPath()}, attrs...)
	args = append(args, "--no-link")
	cmd := nix.Command("build").Args(args).Executor(s.local)
	if !p.Raw {
		cmd = cmd.Reporter(tui.NewBuildReporter(tui.ResolveReporterMode(p.Compact, p.Verbose)))
	}
	out, err := cmd.Run(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build configuration: %w", err)
	}

	// nix build prints out paths in the order the installables were given.
	paths := strings.Fields(string(out))
	if len(paths) != len(attrs) {
		return "", nil, fmt.Errorf("expected %d build outputs, got %d: %v", len(attrs), len(paths), paths)
	}
	return paths[0], paths[1:], nil
}

// pick returns the derivation paths for first (if not empty) and rest, in
// order.
func pick(drvs map[string]string, first string, rest ...string) []string {
	var out []string
	if first != "" {
		out = append(out, drvs[first])
	}
	for _, n := range rest {
		out = append(out, drvs[n])
	}
	return out
}

func (s *Session) evalDrvPaths(ctx context.Context, names []string) (map[string]string, error) {
	attr := fmt.Sprintf("systems.nixos.\"%s\".result.config.system.build", s.Plan.Name)
	out, err := nix.Command("eval").
		Args([]string{"-f", s.Plan.Source.FullNillaPath(), attr, "--json", "--apply", drvPathsApply(names)}).
		Executor(s.local).
		Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get derivation paths: %w", err)
	}
	var paths []string
	if err := json.Unmarshal(out, &paths); err != nil {
		return nil, fmt.Errorf("failed to parse derivation paths: %w", err)
	}
	if len(paths) != len(names) {
		return nil, fmt.Errorf("expected %d derivation paths, got %d", len(names), len(paths))
	}
	drvs := make(map[string]string, len(names))
	for i, n := range names {
		drvs[n] = paths[i]
	}
	return drvs, nil
}

func (s *Session) copyDerivations(ctx context.Context, storeURL string, drvs []string) error {
	log.Debugf("Copying derivations to %s", storeURL)
	_, err := nix.Command("copy").
		Args(append([]string{"--to", storeURL, "--derivation", "-s"}, drvs...)).
		Executor(s.nixRemoteExecutor()).
		Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to copy derivations to %s: %w", storeURL, err)
	}
	return nil
}

// buildRemote copies derivations to a builder's store and builds them there,
// returning their output paths in order.
func (s *Session) buildRemote(ctx context.Context, storeURL string, drvs ...string) ([]string, error) {
	if err := s.copyDerivations(ctx, storeURL, drvs); err != nil {
		return nil, err
	}
	args := append([]string{"--store", storeURL, "--no-link"}, outputInstallables(drvs)...)
	return s.runBuild(ctx, s.nixRemoteExecutor(), args, len(drvs))
}

// buildOnTarget copies derivations to the target and builds them by running
// nix there, into the store under root, or the installer's own store if root
// is empty.
func (s *Session) buildOnTarget(ctx context.Context, user, host, port, root string, extraArgs []string, drvs ...string) ([]string, error) {
	if err := s.copyDerivations(ctx, remoteStoreURL(user, host, port, root), drvs); err != nil {
		return nil, err
	}
	args := targetBuildArgs(root, extraArgs, drvs)
	return s.runBuild(ctx, s.target, args, len(drvs))
}

func targetBuildArgs(root string, extraArgs, drvs []string) []string {
	var args []string
	if root != "" {
		args = append(args, "--store", root)
	}
	args = append(args, "--no-link")
	args = append(args, extraArgs...)
	return append(args, outputInstallables(drvs)...)
}

func (s *Session) runBuild(ctx context.Context, ex exec.Executor, args []string, n int) ([]string, error) {
	p := s.Plan
	log.Debugf("Nix build arguments: %v", args)
	cmd := nix.Command("build").Args(args).Executor(ex)
	if !p.Raw {
		cmd = cmd.Reporter(tui.NewBuildReporter(tui.ResolveReporterMode(p.Compact, p.Verbose)))
	}
	out, err := cmd.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build configuration: %w", err)
	}

	paths := strings.Fields(string(out))
	if len(paths) != n {
		return nil, fmt.Errorf("expected %d build outputs, got %d: %v", n, len(paths), paths)
	}
	return paths, nil
}

// buildSystemOnTarget builds the system straight into the store under root
// on the target, so it does not have to fit in the installer's memory or be
// copied again by nixos-install.
func (s *Session) buildSystemOnTarget(ctx context.Context, info *installInfo, drv, user, host, port, root string) (string, error) {
	fmt.Fprintln(os.Stderr)
	printSection(fmt.Sprintf("Building system into %s on target", root))
	paths, err := s.buildOnTarget(ctx, user, host, port, root, substituterArgs(info), drv)
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

func (s *Session) copyTo(ctx context.Context, storeURL string, substitute bool, paths ...string) error {
	args := []string{"--to", storeURL}
	if s.Plan.BuildTarget != "" && s.Plan.BuildTarget != s.Plan.DeployTarget {
		// Built on a separate builder, copy from there.
		args = append(args, "--from", s.Plan.StoreAddr)
	}
	if substitute {
		args = append(args, "--substitute-on-destination")
	}
	args = append(args, paths...)
	log.Debugf("Nix copy arguments: %v", args)

	cmd := nix.Command("copy").Args(args).Executor(s.CopyExecutor())
	if !s.Plan.Raw {
		cmd = cmd.Reporter(tui.NewCopyReporter(tui.ResolveReporterMode(s.Plan.Compact, s.Plan.Verbose)))
	}
	_, err := cmd.Run(ctx)
	return err
}

func (s *Session) copyExtraFiles(ctx context.Context, dir, root string) error {
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(writeExtraFilesArchive(pw, dir))
	}()
	err := stream(ctx, s.target, pr, "tar", "-C", root, "-xpf", "-", "--numeric-owner", "--no-overwrite-dir")
	pr.Close()
	return err
}

func (s *Session) printInstallSummary(mode DiskMode, root string, opts InstallOptions, disks []diskState, placement buildPlacement, buildLater bool) {
	fmt.Fprintln(os.Stderr)
	printSection("Install summary")

	reboot := "yes"
	if opts.NoReboot {
		reboot = "no"
	}
	extra := "-"
	if opts.ExtraFiles != "" {
		extra = opts.ExtraFiles
	}
	build := placement.String()
	if placement == buildOnBuilder {
		build = s.Plan.BuildTarget
	}
	if buildLater {
		build += " (after preparing disks)"
	}
	fmt.Fprintln(os.Stderr, util.RenderTable(
		[]string{"", ""},
		[]string{"System", s.Plan.Name},
		[]string{"Target", s.Plan.DeployTarget},
		[]string{"Build", build},
		[]string{"Disks", string(mode)},
		[]string{"Root", root},
		[]string{"Extra files", extra},
		[]string{"Reboot", reboot},
	))

	if len(disks) > 0 {
		rows := make([][]string, 0, len(disks))
		for _, d := range disks {
			rows = append(rows, []string{d.Name, d.Device, d.Info})
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, util.RenderTable([]string{"Disk", "Device", "Size/Model"}, rows...))
	}
}

func confirmInstall(skip bool, name string, mode DiskMode) (bool, error) {
	if skip {
		return true, nil
	}
	if mode.Wipes() {
		warning := "All data on the disks above will be destroyed."
		if mode == DiskFormat {
			warning = "Missing partitions and filesystems will be created on the disks above, which can overwrite data disko does not know about."
		}
		fmt.Fprintf(os.Stderr, "\n\033[31m%s\033[0m\n", warning)
		ok, err := tui.RunTypedConfirm(fmt.Sprintf("Type the system name (%s) to continue:", name), name)
		if err == nil && !ok {
			log.Info("Name did not match, aborting")
		}
		return ok, err
	}
	return tui.RunConfirm("Do you want to continue?")
}
