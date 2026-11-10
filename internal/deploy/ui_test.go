package deploy

import (
	"testing"

	"github.com/go-test/deep"
)

func TestPlannedStages(t *testing.T) {
	tests := []struct {
		name string
		plan *Plan
		want []stage
	}{
		{
			name: "local build",
			plan: &Plan{SubCmd: Build},
			want: []stage{stageBuild, stageDiff},
		},
		{
			name: "local build without diff",
			plan: &Plan{SubCmd: Build, NoDiff: true},
			want: []stage{stageBuild},
		},
		{
			name: "local switch",
			plan: &Plan{SubCmd: Switch},
			want: []stage{stageBuild, stageDiff, stageConfirm},
		},
		{
			name: "switch with --confirm skips the prompt",
			plan: &Plan{SubCmd: Switch, Confirm: true},
			want: []stage{stageBuild, stageDiff},
		},
		{
			name: "switch on a remote target copies",
			plan: &Plan{SubCmd: Switch, DeployTarget: "root@host"},
			want: []stage{stageBuild, stageDiff, stageConfirm, stageCopy},
		},
		{
			name: "remote build on the target needs no copy",
			plan: &Plan{
				SubCmd:       Switch,
				DeployTarget: "root@host",
				BuildTarget:  "root@host",
				StoreAddr:    "ssh-ng://root@host",
			},
			want: []stage{stageEval, stageCopyDrv, stageBuild, stageDiff, stageConfirm},
		},
		{
			name: "remote build elsewhere copies",
			plan: &Plan{
				SubCmd:       Test,
				DeployTarget: "root@host",
				BuildTarget:  "root@builder",
				StoreAddr:    "ssh-ng://root@builder",
			},
			want: []stage{stageEval, stageCopyDrv, stageBuild, stageDiff, stageConfirm, stageCopy},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := deep.Equal(plannedStages(tt.plan), tt.want); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestExtractStoreName(t *testing.T) {
	got := extractStoreName("/nix/store/abc123-nixos-system-host.drv")
	if got != "nixos-system-host.drv" {
		t.Errorf("got %q", got)
	}
}
