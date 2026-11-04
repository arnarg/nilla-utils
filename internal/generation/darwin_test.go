package generation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func (h *fakeHost) addDarwinGen(id int, version string, mtime time.Time) {
	base := fmt.Sprintf("%s/system-%d-link", nixosProfilesDir, id)
	h.entries[base] = fakeEntry{kind: kSymlink, target: fmt.Sprintf("/nix/store/darwin%d", id), mtime: mtime}
	if version != "" {
		h.entries[base+"/darwin-version"] = fakeEntry{kind: kFile, content: version + "\n", mtime: mtime}
	}
}

func TestDarwinSystem_ListAndCurrent(t *testing.T) {
	h := newFakeHost(true)
	t0 := time.Unix(1700000000, 0)
	t1 := t0.Add(time.Hour)
	h.addDarwinGen(1, "25.05.abc1234", t0)
	h.addDarwinGen(2, "25.11.def5678", t1)
	h.setNixOSCurrent(2, t1)

	gens, err := DarwinSystem{}.List(h)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(gens) != 2 {
		t.Fatalf("expected 2 generations, got %d", len(gens))
	}
	if gens[0].ID != 1 || gens[0].Version != "25.05.abc1234" {
		t.Errorf("unexpected first generation: %+v", gens[0])
	}

	cur, err := DarwinSystem{}.Current(h)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.ID != 2 || cur.Version != "25.11.def5678" {
		t.Errorf("unexpected current generation: %+v", cur)
	}

	row := DarwinSystem{}.Row(cur)
	if len(row) != len(DarwinSystem{}.Headers()) {
		t.Errorf("row %v does not match headers %v", row, DarwinSystem{}.Headers())
	}
}

func TestDarwinSystem_MissingVersion(t *testing.T) {
	h := newFakeHost(true)
	h.addDarwinGen(1, "", time.Unix(1700000000, 0))

	gens, err := DarwinSystem{}.List(h)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(gens) != 1 || gens[0].Version != "Unknown" {
		t.Errorf("expected one generation with Unknown version, got %+v", gens)
	}
}

func TestDarwinSystem_Rollback(t *testing.T) {
	gen := Generation{ID: 3, path: nixosProfilesDir + "/system-3-link"}
	wantScript := fmt.Sprintf(
		"%s; nix-env -p %q --switch-generation 3 && exec %q",
		darwinPath, nixosCurrentLink, gen.path+"/activate",
	)

	t.Run("local runs without sudo", func(t *testing.T) {
		h := newFakeHost(true)
		if err := (DarwinSystem{}).Rollback(context.Background(), h, gen); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		want := "/bin/sh -c " + wantScript
		if len(h.ranCmds) != 1 || h.ranCmds[0] != want {
			t.Errorf("ran %v, want [%s]", h.ranCmds, want)
		}
	})

	t.Run("remote runs a single sudo shell", func(t *testing.T) {
		h := newFakeHost(false)
		if err := (DarwinSystem{}).Rollback(context.Background(), h, gen); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		if len(h.ranCmds) != 1 || !strings.HasPrefix(h.ranCmds[0], "sudo /bin/sh -c ") {
			t.Errorf("ran %v, want one sudo /bin/sh -c command", h.ranCmds)
		}
	})
}
