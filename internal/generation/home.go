package generation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/util"
)

var (
	homeGenIDRe   = regexp.MustCompile(`^home-manager-(\d+)-link$`)
	homeGenListRe = regexp.MustCompile(`^home-manager-\d+-link$`)
)

// fsProbe is the read-only subset of exec.Executor / exec.Host needed to locate
// the current Home Manager generation. Both exec.Executor and exec.Host satisfy
// it, so the deploy path (which holds an exec.Executor) and the Host-based
// generation commands share the same discovery logic.
type fsProbe interface {
	Command(string, ...string) (exec.Command, error)
	PathExists(string) (bool, error)
	IsLocal() bool
}

// HomeSystem implements System for Home Manager generations.
type HomeSystem struct{}

func (HomeSystem) RequiresLocalRoot() bool { return false }

func (HomeSystem) Headers() []string {
	return []string{"Generation", "Build date", "Home Manager version"}
}

func (HomeSystem) Row(g Generation) []string {
	return []string{
		strconv.Itoa(g.ID),
		g.BuildDate.Format(time.DateTime),
		g.Version,
	}
}

func (HomeSystem) Current(h exec.Host) (Generation, error) {
	path, ok := currentHomeLinkPath(h, "")
	if !ok {
		return Generation{}, errors.New("current generation not found")
	}
	ei, err := h.Lstat(path)
	if err != nil {
		return Generation{}, err
	}
	return buildHomeGeneration(h, filepath.Dir(path), ei)
}

func (HomeSystem) List(h exec.Host) ([]Generation, error) {
	for _, dir := range homeProfileDirs(h) {
		gens, err := listHomeGenerations(h, dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if len(gens) > 0 {
			return gens, nil
		}
	}
	return []Generation{}, nil
}

func (HomeSystem) DeleteGenerations(h exec.Host, gens []Generation) error {
	for _, g := range gens {
		if err := h.Remove(g.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (HomeSystem) CollectGarbage(ctx context.Context, h exec.Host) error {
	// Home generations are user-owned; no elevation is required locally or
	// remotely. The gc runs against the host's own store.
	return runGC(ctx, h, "nix", "store", "gc", "-v")
}

func buildHomeGeneration(h exec.Host, dir string, e exec.EntryInfo) (Generation, error) {
	m := homeGenIDRe.FindStringSubmatch(e.Name)
	if m == nil {
		return Generation{}, fmt.Errorf("generation path '%s' does not match pattern", e.Name)
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		return Generation{}, err
	}

	path := filepath.Join(dir, e.Name)

	verBytes, err := h.ReadFile(filepath.Join(path, "hm-version"))
	if err != nil {
		return Generation{}, err
	}

	return Generation{
		ID:        id,
		BuildDate: e.ModTime,
		Version:   strings.TrimSpace(string(verBytes)),
		path:      path,
	}, nil
}

func listHomeGenerations(h exec.Host, dir string) ([]Generation, error) {
	entries, err := h.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var gens []Generation
	for _, e := range entries {
		if !e.IsSymlink || !homeGenListRe.MatchString(e.Name) {
			continue
		}
		g, err := buildHomeGeneration(h, dir, e)
		if err != nil {
			return nil, err
		}
		gens = append(gens, g)
	}
	return gens, nil
}

// currentHomeLinkPath finds the current home-manager generation path. A non-empty
// userHint (e.g. parsed from a system name) takes precedence over discovery.
func currentHomeLinkPath(p fsProbe, userHint string) (string, bool) {
	user, home := homeUserAndDir(p, userHint)
	for _, dir := range homeDirs(user, home) {
		link := filepath.Join(dir, "home-manager")
		exists, err := p.PathExists(link)
		if err != nil || !exists {
			continue
		}
		out, err := runOutput(p, "readlink", link)
		if err != nil {
			continue
		}
		target := strings.TrimSpace(out)
		if target == "" {
			continue
		}
		return filepath.Join(dir, target), true
	}
	return "", false
}

// homeProfileDirs returns the candidate profile directories for the host backing
// p, discovering the remote user when necessary.
func homeProfileDirs(p fsProbe) []string {
	user, home := homeUserAndDir(p, "")
	return homeDirs(user, home)
}

func homeUserAndDir(p fsProbe, userHint string) (user, home string) {
	switch {
	case userHint != "":
		user = userHint
	case p.IsLocal():
		user = util.GetUser()
	default:
		if u, err := runOutput(p, "id", "-un"); err == nil {
			user = strings.TrimSpace(u)
		}
	}
	if p.IsLocal() {
		home = util.GetHomeDir()
	} else {
		home = remoteHomeDir(p, user)
	}
	return user, home
}

func homeDirs(user, home string) []string {
	var dirs []string
	if user != "" {
		dirs = append(dirs, fmt.Sprintf("/nix/var/nix/profiles/per-user/%s", user))
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "state", "nix", "profiles"))
	}
	return dirs
}

// remoteHomeDir resolves a remote user's home directory via getent (Linux) with a
// printenv HOME fallback (macOS and Linux).
func remoteHomeDir(p fsProbe, user string) string {
	if out, err := runOutput(p, "getent", "passwd", user); err == nil {
		if fields := strings.Split(strings.TrimSpace(out), ":"); len(fields) >= 6 && fields[5] != "" {
			return fields[5]
		}
	}
	if out, err := runOutput(p, "printenv", "HOME"); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func runOutput(p fsProbe, name string, args ...string) (string, error) {
	cmd, err := p.Command(name, args...)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	cmd.SetStdout(&buf)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// CurrentHomeGenerationPath locates the current Home Manager generation path for
// the given executor. When username is empty the local user (or, for remote
// executors, the discovered remote user) is used. It is retained for the deploy
// path, which operates on an exec.Executor rather than a full exec.Host.
func CurrentHomeGenerationPath(e exec.Executor, username string) (string, bool) {
	return currentHomeLinkPath(e, username)
}
