package deploy

import (
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
