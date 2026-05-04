package deploy

import (
	"testing"

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
