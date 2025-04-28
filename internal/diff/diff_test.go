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
			set:  PackageSet{map[string]set.Unordered[string]{}},
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
		out  []string
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
			out: []string{"1.13", "1.13-lib"},
		},
		{
			name: "package does not exist",
			set:  PackageSet{map[string]set.Unordered[string]{}},
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
