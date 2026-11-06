package deploy

import (
	"errors"
	"fmt"
	"testing"

	"github.com/arnarg/nilla-utils/internal/generation"
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

}

func TestResolveLocalHostName(t *testing.T) {
	ok := func() ([]byte, error) { return []byte("mymac\n"), nil }

	t.Run("uses scutil on darwin", func(t *testing.T) {
		name, err := resolveLocalHostName("darwin", ok)
		if err != nil {
			t.Fatal(err)
		}
		if name != "mymac" {
			t.Errorf("got %q, want %q", name, "mymac")
		}
	})

	t.Run("requires a name outside darwin", func(t *testing.T) {
		called := false
		_, err := resolveLocalHostName("linux", func() ([]byte, error) {
			called = true
			return ok()
		})
		if err == nil {
			t.Fatal("expected an error")
		}
		if called {
			t.Error("scutil should not run outside darwin")
		}
	})

	t.Run("errors on scutil failure", func(t *testing.T) {
		_, err := resolveLocalHostName("darwin", func() ([]byte, error) {
			return nil, errors.New("boom")
		})
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("errors on empty LocalHostName", func(t *testing.T) {
		_, err := resolveLocalHostName("darwin", func() ([]byte, error) {
			return []byte("  \n"), nil
		})
		if err == nil {
			t.Fatal("expected an error")
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
		generation.DarwinPathExport, systemProfile, out, out+"/activate",
	)
	if got[3] != want {
		t.Errorf("script = %q, want %q", got[3], want)
	}
}
