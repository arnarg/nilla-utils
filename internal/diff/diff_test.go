package diff

import (
	"testing"

	"github.com/go-test/deep"
	"github.com/s0rg/set"
)

func TestParsePackageFromPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  *Package
	}{
		{
			name: "package with simple version",
			in:   "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13",
			out: &Package{
				pname:   "gzip",
				version: "1.13",
				path:    "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13",
			},
		},
		{
			name: "package with output suffix",
			in:   "/nix/store/3bl0g75vyjgg8gnggwiavbwdxyg6gv20-gnutar-1.35-info",
			out: &Package{
				pname:   "gnutar",
				version: "1.35-info",
				path:    "/nix/store/3bl0g75vyjgg8gnggwiavbwdxyg6gv20-gnutar-1.35-info",
			},
		},
		{
			name: "package without version",
			in:   "/nix/store/i6fl7i35dacvxqpzya6h78nacciwryfh-nixos-rebuild",
			out: &Package{
				pname:   "nixos-rebuild",
				version: "",
				path:    "/nix/store/i6fl7i35dacvxqpzya6h78nacciwryfh-nixos-rebuild",
			},
		},
		{
			name: "package with .drv suffix",
			in:   "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13.drv",
			out: &Package{
				pname:   "gzip",
				version: "1.13",
				path:    "/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := ParsePackageFromPath(tt.in)

			if diff := deep.Equal(pkg, tt.out); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestNewPackageSet(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		out  PackageSet
	}{
		{
			name: "valid store paths",
			in: []string{
				"/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13",
				"/nix/store/nc394xps4al1r99ziabqvajbkrhxr5b7-gzip-1.13-lib",
				"/nix/store/3bl0g75vyjgg8gnggwiavbwdxyg6gv20-gnutar-1.35-info",
			},
			out: PackageSet{
				packages: map[string]set.Unordered[string]{
					"gzip": {
						"1.13":     true,
						"1.13-lib": true,
					},
					"gnutar": {
						"1.35-info": true,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := NewPackageSet(tt.in)

			if diff := deep.Equal(pkg, tt.out); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestHasPackage(t *testing.T) {
	tests := []struct {
		name string
		set  PackageSet
		in   string
		out  bool
	}{
		{
			name: "package exists",
			set: PackageSet{
				packages: map[string]set.Unordered[string]{
					"gzip": {
						"1.13":     true,
						"1.13-lib": true,
					},
					"gnutar": {
						"1.35-info": true,
					},
				},
			},
			in:  "gzip",
			out: true,
		},
		{
			name: "package does not exist",
			set:  PackageSet{set.Unordered[string]{}, map[string]set.Unordered[string]{}},
			in:   "gzip",
			out:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.set.HasPackage(tt.in)

			if diff := deep.Equal(result, tt.out); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestGetPackagesVersions(t *testing.T) {
	tests := []struct {
		name string
		set  PackageSet
		in   string
		out  set.Unordered[string]
	}{
		{
			name: "package exists",
			set: PackageSet{
				pnames: set.Unordered[string]{"gzip": true, "gnutar": true},
				packages: map[string]set.Unordered[string]{
					"gzip": {
						"1.13":     true,
						"1.13-lib": true,
					},
					"gnutar": {
						"1.35-info": true,
					},
				},
			},
			in:  "gzip",
			out: set.Unordered[string]{"1.13": true, "1.13-lib": true},
		},
		{
			name: "package does not exist",
			set:  PackageSet{set.Unordered[string]{}, map[string]set.Unordered[string]{}},
			in:   "gzip",
			out:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.set.GetPackagesVersions(tt.in)

			if diff := deep.Equal(result, tt.out); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestCalculate(t *testing.T) {
	tests := []struct {
		name string
		from PackageSet
		to   PackageSet
		out  Diff
	}{
		{
			name: "diff with all possible changes",
			from: PackageSet{
				pnames: set.Unordered[string]{"gzip": true, "gnutar": true},
				packages: map[string]set.Unordered[string]{
					"gzip": {
						"1.13":     true,
						"1.13-lib": true,
					},
					"gnutar": {
						"1.35-info": true,
					},
				},
			},
			to: PackageSet{
				pnames: set.Unordered[string]{"gzip": true, "tar": true},
				packages: map[string]set.Unordered[string]{
					"gzip": {
						"1.14":     true,
						"1.14-lib": true,
					},
					"tar": {
						"1.35-info": true,
					},
				},
			},
			out: Diff{
				Changed: []PackageDiff{
					{
						PName:  "gzip",
						Before: []string{"1.13", "1.13-lib"},
						After:  []string{"1.14", "1.14-lib"},
					},
				},
				Added: []PackageDiff{
					{
						PName:  "tar",
						Before: []string{},
						After:  []string{"1.35-info"},
					},
				},
				Removed: []PackageDiff{
					{
						PName:  "gnutar",
						Before: []string{"1.35-info"},
						After:  []string{},
					},
				},
			},
		},
		{
			name: "diff with no changes",
			from: PackageSet{
				pnames: set.Unordered[string]{"gzip": true, "gnutar": true},
				packages: map[string]set.Unordered[string]{
					"gzip": {
						"1.13":     true,
						"1.13-lib": true,
					},
					"gnutar": {
						"1.35-info": true,
					},
				},
			},
			to: PackageSet{
				pnames: set.Unordered[string]{"gzip": true, "gnutar": true},
				packages: map[string]set.Unordered[string]{
					"gzip": {
						"1.13":     true,
						"1.13-lib": true,
					},
					"gnutar": {
						"1.35-info": true,
					},
				},
			},
			out: Diff{
				Changed: []PackageDiff{},
				Added:   []PackageDiff{},
				Removed: []PackageDiff{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Calculate(tt.from, tt.to)

			if diff := deep.Equal(result, tt.out); diff != nil {
				t.Error(diff)
			}
		})
	}
}

func TestFindLongestPrefixIndex(t *testing.T) {
	tests := []struct {
		name     string
		before   []string
		after    []string
		expected int
	}{
		{
			name:     "simple patch version bump",
			before:   []string{"1.2.3"},
			after:    []string{"1.2.4"},
			expected: 4,
		},
		{
			name:     "simple minor version bump",
			before:   []string{"1.2.0"},
			after:    []string{"1.3.0"},
			expected: 2,
		},
		{
			name:     "simple major version bump",
			before:   []string{"1.2.0"},
			after:    []string{"2.1.0"},
			expected: 0,
		},
		{
			name:     "version containing dash",
			before:   []string{"2.4.11-lib"},
			after:    []string{"2.4.11-man"},
			expected: 7,
		},
		{
			name:     "multiple versions mixed",
			before:   []string{"2.4.11", "2.4.11-man"},
			after:    []string{"2.5", "2.5-man", "2.5-lib"},
			expected: 2,
		},
		{
			name:     "multiple versions mixed unchanged",
			before:   []string{"2.4.11", "2.4.11-man"},
			after:    []string{"2.4.11", "2.4.11-man"},
			expected: 6,
		},
		{
			name:     "multiple versions with empty ones",
			before:   []string{"", "257.7"},
			after:    []string{"", "257.8"},
			expected: 4,
		},
		{
			name:     "only empty version strings",
			before:   []string{""},
			after:    []string{""},
			expected: 0,
		},
		{
			name:     "only actual version removed",
			before:   []string{"", "257.7"},
			after:    []string{"", ""},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findLongestPrefixIndex(tt.before, tt.after)

			if tt.expected != result {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}
