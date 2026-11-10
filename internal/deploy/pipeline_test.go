package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arnarg/nilla-utils/internal/exec"

	"github.com/arnarg/nilla-utils/internal/project"
	"github.com/go-test/deep"
)

func testSource() *project.ProjectSource {
	return &project.ProjectSource{
		NillaPath: "./nilla.nix",
		StorePath: "/nix/store/abc123-myproject",
		StoreHash: "sha256-xyz",
	}
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name string
		plan *Plan
		want []string
	}{
		{
			name: "build command with no-link",
			plan: &Plan{
				Source: testSource(),
				Attr:   "systems.nixos.\"myhost\".result",
				SubCmd: Build,
				NoLink: true,
			},
			want: []string{
				"-f", "/nix/store/abc123-myproject/nilla.nix",
				"systems.nixos.\"myhost\".result",
				"--no-link",
			},
		},
		{
			name: "build command with out-link",
			plan: &Plan{
				Source:  testSource(),
				Attr:    "systems.nixos.\"myhost\".result",
				SubCmd:  Build,
				OutLink: "/tmp/result",
			},
			want: []string{
				"-f", "/nix/store/abc123-myproject/nilla.nix",
				"systems.nixos.\"myhost\".result",
				"--out-link", "/tmp/result",
			},
		},
		{
			name: "build command with no flags creates default result symlink",
			plan: &Plan{
				Source: testSource(),
				Attr:   "systems.nixos.\"myhost\".result",
				SubCmd: Build,
			},
			want: []string{
				"-f", "/nix/store/abc123-myproject/nilla.nix",
				"systems.nixos.\"myhost\".result",
			},
		},
		{
			name: "out-link takes precedence over no-link when both set during build",
			plan: &Plan{
				Source:  testSource(),
				Attr:    "systems.nixos.\"myhost\".result",
				SubCmd:  Build,
				NoLink:  true,
				OutLink: "/tmp/result",
			},
			want: []string{
				"-f", "/nix/store/abc123-myproject/nilla.nix",
				"systems.nixos.\"myhost\".result",
				"--out-link", "/tmp/result",
			},
		},
		{
			name: "test command always gets no-link",
			plan: &Plan{
				Source: testSource(),
				Attr:   "systems.nixos.\"myhost\".result",
				SubCmd: Test,
			},
			want: []string{
				"-f", "/nix/store/abc123-myproject/nilla.nix",
				"systems.nixos.\"myhost\".result",
				"--no-link",
			},
		},
		{
			name: "switch command ignores no-link and out-link flags",
			plan: &Plan{
				Source:  testSource(),
				Attr:    "systems.nixos.\"myhost\".result",
				SubCmd:  Switch,
				NoLink:  true,
				OutLink: "/tmp/result",
			},
			want: []string{
				"-f", "/nix/store/abc123-myproject/nilla.nix",
				"systems.nixos.\"myhost\".result",
				"--no-link",
			},
		},
		{
			name: "remote build adds store and eval-store flags",
			plan: &Plan{
				Source:      testSource(),
				Attr:        "systems.nixos.\"myhost\".result",
				SubCmd:      Test,
				BuildTarget: "user@builder",
				StoreAddr:   "ssh-ng://user@builder",
			},
			want: []string{
				"-f", "/nix/store/abc123-myproject/nilla.nix",
				"systems.nixos.\"myhost\".result",
				"--no-link",
				"--store", "ssh-ng://user@builder",
				"--eval-store", "auto",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs(tt.plan)
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Error(diff)
			}
		})
	}
}

type noGenerationSystem struct{ HomeSystem }

func (noGenerationSystem) CurrentGeneration(_ exec.Executor, _ string) (*Generation, error) {
	return nil, fmt.Errorf("wrapped: %w", ErrNoCurrentGeneration)
}

func TestSession_DiffSkipsWithoutCurrentGeneration(t *testing.T) {
	s := &Session{Plan: &Plan{SubCmd: Switch}, System: noGenerationSystem{}}
	// outPath is never read: Diff returns before comparing generations.
	if err := s.Diff(context.Background(), "/nix/store/abc123-home-manager-generation"); err != nil {
		t.Fatalf("expected diff to be skipped without error, got: %v", err)
	}
}

// runBuildWithStubNix runs a build session against a stub nix that prints
// outPath, and returns what the session wrote to stdout.
func runBuildWithStubNix(t *testing.T, printOutPaths bool) string {
	t.Helper()
	const outPath = "/nix/store/abc123-nixos-system-host"

	bin := t.TempDir()
	stub := fmt.Sprintf("#!/bin/sh\necho %s\n", outPath)
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	s := &Session{
		Plan: &Plan{
			Source:        testSource(),
			Attr:          "systems.nixos.host",
			SubCmd:        Build,
			Raw:           true,
			NoDiff:        true,
			PrintOutPaths: printOutPaths,
		},
		local: exec.NewLocalExecutor(),
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	runErr := s.Run(context.Background())
	os.Stdout = stdout
	w.Close()
	out, _ := io.ReadAll(r)

	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	return string(out)
}

func TestSession_RunPrintsOutPaths(t *testing.T) {
	got := runBuildWithStubNix(t, true)
	if want := "/nix/store/abc123-nixos-system-host\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestSession_RunPrintsNothingByDefault(t *testing.T) {
	if got := runBuildWithStubNix(t, false); strings.TrimSpace(got) != "" {
		t.Errorf("stdout = %q, want nothing", got)
	}
}
