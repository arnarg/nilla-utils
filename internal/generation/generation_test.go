package generation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/util"
)

const (
	kFile = iota
	kSymlink
	kDir
)

type fakeEntry struct {
	kind    int
	content string
	target  string
	mtime   time.Time
}

// fakeHost is an in-memory exec.Host used to exercise the System
// implementations without a real (or SSH) filesystem.
type fakeHost struct {
	local   bool
	user    string
	homeDir string
	entries map[string]fakeEntry

	removed []string
	ranCmds []string
}

func newFakeHost(local bool) *fakeHost {
	return &fakeHost{
		local:   local,
		user:    "alice",
		homeDir: "/home/alice",
		entries: map[string]fakeEntry{},
	}
}

func (h *fakeHost) IsLocal() bool { return h.local }

func (h *fakeHost) Lstat(path string) (exec.EntryInfo, error) {
	e, ok := h.entries[path]
	if !ok {
		return exec.EntryInfo{}, fmt.Errorf("%s: %w", path, os.ErrNotExist)
	}
	return exec.EntryInfo{
		Name:      filepath.Base(path),
		ModTime:   e.mtime,
		IsSymlink: e.kind == kSymlink,
	}, nil
}

func (h *fakeHost) ReadDir(dir string) ([]exec.EntryInfo, error) {
	dir = strings.TrimSuffix(dir, "/")
	prefix := dir + "/"
	var infos []exec.EntryInfo
	for p, e := range h.entries {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		infos = append(infos, exec.EntryInfo{
			Name:      rest,
			ModTime:   e.mtime,
			IsSymlink: e.kind == kSymlink,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

func (h *fakeHost) Readlink(path string) (string, error) {
	e, ok := h.entries[path]
	if !ok || e.kind != kSymlink {
		return "", fmt.Errorf("%s: not a symlink", path)
	}
	return e.target, nil
}

func (h *fakeHost) ReadFile(path string) ([]byte, error) {
	e, ok := h.entries[path]
	if !ok {
		return nil, fmt.Errorf("%s: %w", path, os.ErrNotExist)
	}
	return []byte(e.content), nil
}

func (h *fakeHost) Remove(path string) error {
	delete(h.entries, path)
	h.removed = append(h.removed, path)
	return nil
}

func (h *fakeHost) PathExists(path string) (bool, error) {
	_, ok := h.entries[path]
	return ok, nil
}

func (h *fakeHost) Close() error { return nil }

func (h *fakeHost) Command(name string, args ...string) (exec.Command, error) {
	return h.CommandContext(context.Background(), name, args...)
}

func (h *fakeHost) CommandContext(_ context.Context, name string, args ...string) (exec.Command, error) {
	h.ranCmds = append(h.ranCmds, strings.Join(append([]string{name}, args...), " "))
	return &fakeCommand{host: h, name: name, args: args}, nil
}

type fakeCommand struct {
	host   *fakeHost
	name   string
	args   []string
	stdout io.Writer
}

func (c *fakeCommand) SetStdin(io.Reader)                 {}
func (c *fakeCommand) SetStdout(w io.Writer)              { c.stdout = w }
func (c *fakeCommand) SetStderr(io.Writer)                {}
func (c *fakeCommand) StdinPipe() (io.WriteCloser, error) { return nopWriteCloser{}, nil }
func (c *fakeCommand) StdoutPipe() (io.Reader, error)     { return nil, errors.New("not implemented") }
func (c *fakeCommand) StderrPipe() (io.Reader, error)     { return nil, errors.New("not implemented") }
func (c *fakeCommand) Start() error                       { return c.Run() }
func (c *fakeCommand) Wait() error                        { return nil }

func (c *fakeCommand) Run() error {
	write := func(s string) {
		if c.stdout != nil {
			c.stdout.Write([]byte(s))
		}
	}
	switch c.name {
	case "readlink":
		t, err := c.host.Readlink(c.args[0])
		if err != nil {
			return err
		}
		write(t)
		return nil
	case "id":
		write(c.host.user)
		return nil
	case "getent":
		// getent passwd <user> -> user:x:uid:gid:gecos:home:shell
		write(fmt.Sprintf("%s:x:1000:1000::%s:/bin/sh", c.args[1], c.host.homeDir))
		return nil
	case "printenv":
		write(c.host.homeDir)
		return nil
	case "rm":
		for _, p := range c.args {
			c.host.Remove(p)
		}
		return nil
	case "sudo":
		if len(c.args) >= 1 && c.args[0] == "rm" {
			for _, p := range c.args[1:] {
				c.host.Remove(p)
			}
			return nil
		}
		// sudo nix store gc -> no-op success
		return nil
	case "nix":
		// nix store gc -> no-op success
		return nil
	}
	return nil
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

func (h *fakeHost) addNixOSGen(id int, version, kernel string, mtime time.Time) {
	base := fmt.Sprintf("%s/system-%d-link", nixosProfilesDir, id)
	h.entries[base] = fakeEntry{kind: kSymlink, target: fmt.Sprintf("/nix/store/sys%d", id), mtime: mtime}
	h.entries[base+"/nixos-version"] = fakeEntry{kind: kFile, content: version, mtime: mtime}
	if kernel != "" {
		h.entries[base+"/kernel-modules/lib/modules/"+kernel] = fakeEntry{kind: kDir, mtime: mtime}
	}
}

func (h *fakeHost) setNixOSCurrent(id int, mtime time.Time) {
	h.entries[nixosCurrentLink] = fakeEntry{
		kind: kSymlink, target: fmt.Sprintf("system-%d-link", id), mtime: mtime,
	}
}

func (h *fakeHost) addHomeGen(dir string, id int, version string, mtime time.Time, current bool) {
	base := fmt.Sprintf("%s/home-manager-%d-link", dir, id)
	h.entries[base] = fakeEntry{kind: kSymlink, target: fmt.Sprintf("/nix/store/hm%d", id), mtime: mtime}
	h.entries[base+"/hm-version"] = fakeEntry{kind: kFile, content: version, mtime: mtime}
	if current {
		h.entries[dir+"/home-manager"] = fakeEntry{
			kind: kSymlink, target: fmt.Sprintf("home-manager-%d-link", id), mtime: mtime,
		}
	}
}

func localHomeDir() string {
	return fmt.Sprintf("/nix/var/nix/profiles/per-user/%s", util.GetUser())
}

func homePerUserDir(h *fakeHost) string {
	return fmt.Sprintf("/nix/var/nix/profiles/per-user/%s", h.user)
}

func TestNixOSSystem_ListAndCurrent(t *testing.T) {
	h := newFakeHost(true)
	t0 := time.Unix(1700000000, 0)
	t1 := time.Unix(1700000001, 0)
	t2 := time.Unix(1700000002, 0)
	h.addNixOSGen(1, "23.05", "6.1.0", t0)
	h.addNixOSGen(2, "23.05", "6.1.1", t1)
	h.addNixOSGen(3, "23.11", "6.6.0", t2)
	h.setNixOSCurrent(3, t2)

	gens, err := NixOSSystem{}.List(h)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(gens) != 3 {
		t.Fatalf("expected 3 generations, got %d", len(gens))
	}
	byID := map[int]Generation{}
	for _, g := range gens {
		byID[g.ID] = g
	}
	if byID[3].Version != "23.11" {
		t.Errorf("gen 3 version: got %q want 23.11", byID[3].Version)
	}
	if byID[3].KernelVersion != "6.6.0" {
		t.Errorf("gen 3 kernel: got %q want 6.6.0", byID[3].KernelVersion)
	}

	cur, err := NixOSSystem{}.Current(h)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.ID != 3 {
		t.Errorf("current id: got %d want 3", cur.ID)
	}
	if !cur.BuildDate.Equal(t2) {
		t.Errorf("current mtime: got %v want %v", cur.BuildDate, t2)
	}
}

func TestNixOSSystem_UnknownKernel(t *testing.T) {
	// A generation without a kernel-modules dir should report "Unknown" rather
	// than fail the whole listing.
	h := newFakeHost(true)
	h.addNixOSGen(1, "23.05", "", time.Unix(1700000000, 0))
	h.setNixOSCurrent(1, time.Unix(1700000000, 0))

	gens, err := NixOSSystem{}.List(h)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gens[0].KernelVersion != "Unknown" {
		t.Errorf("expected Unknown kernel, got %q", gens[0].KernelVersion)
	}
}

func TestBuildNixOSGeneration_BadNameDoesNotPanic(t *testing.T) {
	// Regression: FindStringSubmatch used to be indexed without a nil check.
	h := newFakeHost(true)
	ei := exec.EntryInfo{Name: "not-a-generation-link", IsSymlink: true}
	_, err := buildNixOSGeneration(h, nixosProfilesDir, ei)
	if err == nil {
		t.Fatal("expected error for malformed name")
	}
}

func TestBuildHomeGeneration_BadNameDoesNotPanic(t *testing.T) {
	h := newFakeHost(true)
	ei := exec.EntryInfo{Name: "not-a-home-link", IsSymlink: true}
	_, err := buildHomeGeneration(h, "/some/dir", ei)
	if err == nil {
		t.Fatal("expected error for malformed name")
	}
}

func TestNixOSSystem_DeleteGenerations_Local(t *testing.T) {
	h := newFakeHost(true)
	h.addNixOSGen(1, "23.05", "6.1.0", time.Unix(1700000000, 0))
	h.addNixOSGen(2, "23.05", "6.1.1", time.Unix(1700000001, 0))
	gens, _ := NixOSSystem{}.List(h)

	if err := (NixOSSystem{}).DeleteGenerations(h, gens); err != nil {
		t.Fatalf("DeleteGenerations: %v", err)
	}
	if len(h.removed) != 2 {
		t.Errorf("expected 2 removals, got %d", len(h.removed))
	}
	// No sudo command should run locally.
	for _, c := range h.ranCmds {
		if strings.HasPrefix(c, "sudo") {
			t.Errorf("local delete ran sudo: %s", c)
		}
	}
}

func TestNixOSSystem_DeleteGenerations_RemoteBatched(t *testing.T) {
	h := newFakeHost(false)
	h.addNixOSGen(1, "23.05", "6.1.0", time.Unix(1700000000, 0))
	h.addNixOSGen(2, "23.05", "6.1.1", time.Unix(1700000001, 0))
	gens, _ := NixOSSystem{}.List(h)

	if err := (NixOSSystem{}).DeleteGenerations(h, gens); err != nil {
		t.Fatalf("DeleteGenerations: %v", err)
	}
	// Exactly one sudo rm command, carrying both paths.
	sudoCount := 0
	for _, c := range h.ranCmds {
		if strings.HasPrefix(c, "sudo rm ") {
			sudoCount++
			for _, g := range gens {
				if !strings.Contains(c, g.Path()) {
					t.Errorf("batched rm missing %s: %s", g.Path(), c)
				}
			}
		}
	}
	if sudoCount != 1 {
		t.Errorf("expected 1 batched sudo rm, got %d (cmds: %v)", sudoCount, h.ranCmds)
	}
}

func TestNixOSSystem_CollectGarbage(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		h := newFakeHost(true)
		if err := (NixOSSystem{}).CollectGarbage(context.Background(), h); err != nil {
			t.Fatalf("CollectGarbage: %v", err)
		}
		if !slices.Contains(h.ranCmds, "nix store gc -v") {
			t.Errorf("expected local nix store gc, got %v", h.ranCmds)
		}
	})
	t.Run("remote", func(t *testing.T) {
		h := newFakeHost(false)
		if err := (NixOSSystem{}).CollectGarbage(context.Background(), h); err != nil {
			t.Fatalf("CollectGarbage: %v", err)
		}
		if !slices.Contains(h.ranCmds, "sudo nix store gc -v") {
			t.Errorf("expected remote sudo nix store gc, got %v", h.ranCmds)
		}
	})
}

func TestHomeSystem_ListAndCurrent_Local(t *testing.T) {
	h := newFakeHost(true)
	dir := localHomeDir()
	t0 := time.Unix(1700000000, 0)
	t1 := time.Unix(1700000001, 0)
	h.addHomeGen(dir, 1, "23.05", t0, false)
	h.addHomeGen(dir, 2, "23.11", t1, true)

	gens, err := HomeSystem{}.List(h)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(gens) != 2 {
		t.Fatalf("expected 2 generations, got %d", len(gens))
	}

	cur, err := HomeSystem{}.Current(h)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.ID != 2 {
		t.Errorf("current id: got %d want 2", cur.ID)
	}
	if cur.Version != "23.11" {
		t.Errorf("current version: got %q want 23.11", cur.Version)
	}
}

func TestHomeSystem_RemoteUserDiscovery(t *testing.T) {
	// Remote host: the user is discovered via `id -un` and home via getent.
	h := newFakeHost(false)
	dir := homePerUserDir(h)
	h.addHomeGen(dir, 1, "23.05", time.Unix(1700000000, 0), true)

	cur, err := HomeSystem{}.Current(h)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.ID != 1 {
		t.Errorf("current id: got %d want 1", cur.ID)
	}
	if !slices.Contains(h.ranCmds, "id -un") {
		t.Errorf("expected id -un probe, got %v", h.ranCmds)
	}
}

func TestCurrentHomeGenerationPath_ForDeploy(t *testing.T) {
	// deploy-facing helper operates on an exec.Executor (the fakeHost is one).
	h := newFakeHost(true)
	dir := localHomeDir()
	h.addHomeGen(dir, 4, "23.11", time.Unix(1700000000, 0), true)

	path, ok := CurrentHomeGenerationPath(h, "")
	if !ok {
		t.Fatal("expected to find current path")
	}
	want := dir + "/home-manager-4-link"
	if path != want {
		t.Errorf("path: got %q want %q", path, want)
	}
}

func TestNixOSSystem_HeadersAndRow(t *testing.T) {
	sys := NixOSSystem{}
	if len(sys.Headers()) != 4 {
		t.Errorf("expected 4 headers, got %d", len(sys.Headers()))
	}
	if !sys.RequiresLocalRoot() {
		t.Error("NixOS should require local root")
	}
}

func TestHomeSystem_HeadersAndRow(t *testing.T) {
	sys := HomeSystem{}
	if len(sys.Headers()) != 3 {
		t.Errorf("expected 3 headers, got %d", len(sys.Headers()))
	}
	if sys.RequiresLocalRoot() {
		t.Error("Home should not require local root")
	}
}
