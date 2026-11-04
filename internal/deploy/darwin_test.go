package deploy

import (
	"fmt"
	"strings"
	"testing"
)

func TestDarwinSystem_AttrPath(t *testing.T) {
	got := DarwinSystem{}.AttrPath("mymac")
	want := `systems.darwin."mymac".result.config.system.build.toplevel`
	if got != want {
		t.Errorf("AttrPath = %q, want %q", got, want)
	}
}

func TestDarwinSystem_ResolveName(t *testing.T) {
	t.Run("explicit name returned as-is", func(t *testing.T) {
		name, err := DarwinSystem{}.ResolveName("mymac", "/some/path")
		if err != nil {
			t.Fatal(err)
		}
		if name != "mymac" {
			t.Errorf("got %q, want %q", name, "mymac")
		}
	})

	t.Run("empty name resolves without .local suffix", func(t *testing.T) {
		name, err := DarwinSystem{}.ResolveName("", "/some/path")
		if err != nil {
			t.Fatal(err)
		}
		if name == "" || strings.HasSuffix(name, ".local") {
			t.Errorf("got %q, want a non-empty name without .local", name)
		}
	})
}

func TestDarwinActivateArgs(t *testing.T) {
	const out = "/nix/store/abc123-darwin-system-25.11"

	got := darwinActivateArgs(out)
	if len(got) != 4 || got[0] != "sudo" || got[1] != "/bin/sh" || got[2] != "-c" {
		t.Fatalf("got %v, want a single sudo /bin/sh -c invocation", got)
	}
	want := fmt.Sprintf(
		"%s; nix-env -p %q --set %q && exec %q",
		darwinActivatePath, systemProfile, out, out+"/activate",
	)
	if got[3] != want {
		t.Errorf("script = %q, want %q", got[3], want)
	}
}
