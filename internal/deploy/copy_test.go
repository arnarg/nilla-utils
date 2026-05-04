package deploy

import (
	"testing"

	"github.com/go-test/deep"
)

func TestResolveCopy(t *testing.T) {
	outPath := "/nix/store/xxx-myconfig"

	tests := []struct {
		name string
		plan *Plan
		want copyPlan
	}{
		{
			name: "no deploy target skips copy",
			plan: &Plan{},
			want: copyPlan{skip: true},
		},
		{
			name: "build target equals deploy target skips copy",
			plan: &Plan{
				BuildTarget:  "user@host",
				DeployTarget: "user@host",
			},
			want: copyPlan{skip: true},
		},
		{
			name: "local build with deploy target copies to target",
			plan: &Plan{
				DeployTarget: "user@deployhost",
			},
			want: copyPlan{
				args: []string{"--to", "ssh://user@deployhost", outPath},
			},
		},
		{
			name: "remote build on different host copies from build to deploy",
			plan: &Plan{
				BuildTarget:  "builduser@builder",
				DeployTarget: "deployuser@deployhost",
			},
			want: copyPlan{
				args: []string{
					"--to", "ssh://deployuser@deployhost",
					"--from", "ssh-ng://builduser@builder",
					outPath,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCopy(tt.plan, outPath)
			if tt.want.skip {
				if !got.skip {
					t.Error("expected skip=true, got skip=false")
				}
				return
			}
			if got.skip {
				t.Fatal("expected skip=false, got skip=true")
			}
			if diff := deep.Equal(got.args, tt.want.args); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestConfirm(t *testing.T) {
	t.Run("skip returns true without error", func(t *testing.T) {
		ok, err := Confirm(true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected ok=true")
		}
	})
}
