package deploy

import (
	"fmt"
	"slices"
	"testing"
)

func TestNixOSSystem_AttrPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple hostname",
			in:   "myhost",
			want: `systems.nixos."myhost".result.config.system.build.toplevel`,
		},
		{
			name: "hostname with domain",
			in:   "myhost.example.com",
			want: `systems.nixos."myhost.example.com".result.config.system.build.toplevel`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := NixOSSystem{}
			got := sys.AttrPath(tt.in)
			if got != tt.want {
				t.Errorf("AttrPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNixOSSystem_ResolveName(t *testing.T) {
	t.Run("explicit name returned as-is", func(t *testing.T) {
		sys := NixOSSystem{}
		name, err := sys.ResolveName("myhost", "/some/path")
		if err != nil {
			t.Fatal(err)
		}
		if name != "myhost" {
			t.Errorf("got %q, want %q", name, "myhost")
		}
	})

	t.Run("empty name resolves to hostname", func(t *testing.T) {
		sys := NixOSSystem{}
		name, err := sys.ResolveName("", "/some/path")
		if err != nil {
			t.Fatal(err)
		}
		if name == "" {
			t.Error("expected non-empty hostname")
		}
	})
}

func TestSwitchToConfigArgs(t *testing.T) {
	const out = "/nix/store/abc123-nixos-system-host"

	t.Run("test runs switch-to-configuration directly", func(t *testing.T) {
		want := []string{"sudo", out + "/bin/switch-to-configuration", "test"}
		got := switchToConfigArgs(out, "test")
		if !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	for _, action := range []string{"boot", "switch"} {
		t.Run(action+" sets profile in the same privileged shell", func(t *testing.T) {
			got := switchToConfigArgs(out, action)
			if len(got) != 4 {
				t.Fatalf("got %v, want 4 args", got)
			}
			if got[0] != "sudo" || got[1] != "/bin/sh" || got[2] != "-c" {
				t.Errorf("got %v, want a single sudo /bin/sh -c invocation", got)
			}
			want := fmt.Sprintf(
				"nix-env -p %q --set %q && exec %q %s",
				systemProfile, out, out+"/bin/switch-to-configuration", action,
			)
			if got[3] != want {
				t.Errorf("script = %q, want %q", got[3], want)
			}
		})
	}
}
