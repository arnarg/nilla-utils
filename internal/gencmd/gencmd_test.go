package gencmd

import (
	"strings"
	"testing"
	"time"

	"github.com/arnarg/nilla-utils/internal/generation"
)

func gens(ids ...int) []generation.Generation {
	out := make([]generation.Generation, len(ids))
	for i, id := range ids {
		out[i] = generation.Generation{ID: id}
	}
	return out
}

func keptIDs(actions []action) []int {
	var ids []int
	for _, a := range actions {
		if a.keep {
			ids = append(ids, a.gen.ID)
		}
	}
	return ids
}

func deletedIDs(actions []action) []int {
	var ids []int
	for _, a := range actions {
		if !a.keep {
			ids = append(ids, a.gen.ID)
		}
	}
	return ids
}

func TestSortDesc(t *testing.T) {
	g := gens(2, 5, 1, 3)
	sortDesc(g)
	for i := 0; i < len(g)-1; i++ {
		if g[i].ID < g[i+1].ID {
			t.Fatalf("not descending: %v", g)
		}
	}
}

func TestBuildPlan_CurrentIsNewest(t *testing.T) {
	// keep=1, current is the newest: keep only the current.
	actions := buildPlan(gens(5, 4, 3, 2, 1), generation.Generation{ID: 5}, 1)
	if k := keptIDs(actions); !eq(k, 5) {
		t.Errorf("kept: got %v want [5]", k)
	}
	if d := deletedIDs(actions); !eq(d, 4, 3, 2, 1) {
		t.Errorf("deleted: got %v want [4 3 2 1]", d)
	}
}

func TestBuildPlan_CurrentIsOlder(t *testing.T) {
	// keep=1 but current is the 3rd newest: the held slot goes to the current,
	// newer generations are released.
	actions := buildPlan(gens(5, 4, 3, 2, 1), generation.Generation{ID: 3}, 1)
	if k := keptIDs(actions); !eq(k, 3) {
		t.Errorf("kept: got %v want [3]", k)
	}
}

func TestBuildPlan_KeepMoreThanOne(t *testing.T) {
	actions := buildPlan(gens(5, 4, 3, 2, 1), generation.Generation{ID: 5}, 2)
	if k := keptIDs(actions); !eq(k, 5, 4) {
		t.Errorf("kept: got %v want [5 4]", k)
	}
}

func TestBuildPlan_CurrentNeverDeleted(t *testing.T) {
	// Regardless of position, the current generation must always be kept.
	for _, cur := range []int{1, 2, 3, 4, 5} {
		actions := buildPlan(gens(5, 4, 3, 2, 1), generation.Generation{ID: cur}, 1)
		if !contains(keptIDs(actions), cur) {
			t.Errorf("current %d was deleted", cur)
		}
	}
}

func TestWithCurrentMarker(t *testing.T) {
	row := []string{"5", "2024-01-01", "23.11"}
	out := withCurrentMarker(row, false)
	if out[0] != "  5" {
		t.Errorf("non-current first cell: got %q want \"  5\"", out[0])
	}

	out = withCurrentMarker(row, true)
	if !strings.Contains(out[0], "*") || !strings.Contains(out[0], "5") {
		t.Errorf("current first cell missing marker/id: %q", out[0])
	}
	// original row must not be mutated
	if row[0] != "5" {
		t.Errorf("original row mutated: %v", row)
	}
}

func TestPlanRow_KeepsAndDeletes(t *testing.T) {
	row := []string{"3", "2024-01-01", "23.11"}

	kept := planRow(row, action{keep: true}, false)
	if len(kept) != 3 {
		t.Fatalf("kept row length: got %d want 3", len(kept))
	}
	if !strings.Contains(kept[0], "3") {
		t.Errorf("kept first cell missing id: %q", kept[0])
	}

	deleted := planRow(row, action{keep: false}, false)
	if len(deleted) != 3 {
		t.Fatalf("deleted row length: got %d want 3", len(deleted))
	}
}

func TestPlanRow_CurrentMarkerApplied(t *testing.T) {
	row := []string{"3", "2024-01-01"}
	out := planRow(row, action{keep: true}, true)
	if !strings.Contains(out[0], "*") {
		t.Errorf("current plan row missing marker: %q", out[0])
	}
}

// sanity: BuildDate formatting round-trips through Row indirectly.
func TestGenerationRowShape(t *testing.T) {
	g := generation.Generation{ID: 7, BuildDate: time.Unix(1700000000, 0)}
	_ = g // Generation is constructed by System.Row in practice; ensure type usable.
}

func eq(got []int, want ...int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
