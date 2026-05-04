package deploy

import (
	"testing"
)

func TestHomeSystem_AttrPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple username",
			in:   "alice",
			want: `systems.home."alice".result.config.home.activationPackage`,
		},
		{
			name: "user@host format",
			in:   "alice@myhost",
			want: `systems.home."alice@myhost".result.config.home.activationPackage`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := HomeSystem{}
			got := sys.AttrPath(tt.in)
			if got != tt.want {
				t.Errorf("AttrPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHomeSystem_ResolveName(t *testing.T) {
	t.Run("explicit name returned as-is", func(t *testing.T) {
		sys := HomeSystem{}
		name, err := sys.ResolveName("alice@myhost", "/some/path")
		if err != nil {
			t.Fatal(err)
		}
		if name != "alice@myhost" {
			t.Errorf("got %q, want %q", name, "alice@myhost")
		}
	})
}

func TestExtractUsername(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "user@host format",
			in:   "alice@myhost",
			want: "alice",
		},
		{
			name: "user only",
			in:   "alice",
			want: "alice",
		},
		{
			name: "root@host",
			in:   "root@server1",
			want: "root",
		},
		{
			name: "multiple at signs uses first",
			in:   "user@host@extra",
			want: "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUsername(tt.in)
			if got != tt.want {
				t.Errorf("extractUsername(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
