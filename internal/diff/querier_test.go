package diff

import (
	"testing"

	"github.com/go-test/deep"
)

func TestParsePackageFromPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want *Package
	}{
		{
			name: "package with simple version",
			in:   "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13",
			want: &Package{
				Name:    "gzip",
				Version: "1.13",
				Path:    "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13",
			},
		},
		{
			name: "package with output suffix",
			in:   "/nix/store/3bl0g75vyjgg8gnggwiavbwdxyg6gv20-gnutar-1.35-info",
			want: &Package{
				Name:    "gnutar",
				Version: "1.35-info",
				Path:    "/nix/store/3bl0g75vyjgg8gnggwiavbwdxyg6gv20-gnutar-1.35-info",
			},
		},
		{
			name: "package without version",
			in:   "/nix/store/i6fl7i35dacvxqpzya6h78nacciwryfh-nixos-rebuild",
			want: &Package{
				Name:    "nixos-rebuild",
				Version: "",
				Path:    "/nix/store/i6fl7i35dacvxqpzya6h78nacciwryfh-nixos-rebuild",
			},
		},
		{
			name: "package with .drv suffix",
			in:   "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13.drv",
			want: &Package{
				Name:    "gzip",
				Version: "1.13",
				Path:    "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13.drv",
			},
		},
		{
			name: "invalid path",
			in:   "/not/a/nix/store/path",
			want: nil,
		},
		{
			name: "empty string",
			in:   "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePackageFromPath(tt.in)

			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []string
	}{
		{
			name: "multiple lines",
			in:   []byte("line1\nline2\nline3"),
			want: []string{"line1", "line2", "line3"},
		},
		{
			name: "single line",
			in:   []byte("line1"),
			want: []string{"line1"},
		},
		{
			name: "trailing newline",
			in:   []byte("line1\nline2\n"),
			want: []string{"line1", "line2"},
		},
		{
			name: "empty bytes",
			in:   []byte{},
			want: nil,
		},
		{
			name: "only whitespace",
			in:   []byte("   \n\n  "),
			want: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.in)

			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestDecodeClosureSize(t *testing.T) {
	tests := []struct {
		name    string
		in      []byte
		want    int64
		wantErr bool
	}{
		{
			name: "array format",
			in:   []byte(`[{"closureSize": 12345}]`),
			want: 12345,
		},
		{
			name: "object format",
			in:   []byte(`{"/nix/store/xxx": {"closureSize": 67890}}`),
			want: 67890,
		},
		{
			name: "empty array",
			in:   []byte(`[]`),
			want: 0,
		},
		{
			name: "empty object",
			in:   []byte(`{}`),
			want: 0,
		},
		{
			name: "invalid json",
			in:   []byte(`not json`),
			want: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeClosureSize(tt.in)

			if (err != nil) != tt.wantErr {
				t.Errorf("decodeClosureSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("decodeClosureSize() = %v, want %v", got, tt.want)
			}
		})
	}
}
