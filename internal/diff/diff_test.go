package diff

import (
	"cmp"
	"slices"
	"testing"

	"github.com/go-test/deep"
)

// changesEqual compares two slices of Change ignoring order
func changesEqual(a, b []Change) bool {
	if len(a) != len(b) {
		return false
	}
	// Make copies to sort
	ac := make([]Change, len(a))
	bc := make([]Change, len(b))
	copy(ac, a)
	copy(bc, b)

	// Sort by name
	slices.SortFunc(ac, func(x, y Change) int {
		return cmp.Compare(string(x.Name), string(y.Name))
	})
	slices.SortFunc(bc, func(x, y Change) int {
		return cmp.Compare(string(x.Name), string(y.Name))
	})

	return slices.EqualFunc(ac, bc, func(x, y Change) bool {
		return x.Name == y.Name &&
			x.Type == y.Type &&
			slices.Equal(x.Before, y.Before) &&
			slices.Equal(x.After, y.After)
	})
}

func TestCalculatePackageDiff(t *testing.T) {
	tests := []struct {
		name   string
		before []Package
		after  []Package
		want   Report
	}{
		{
			name: "diff with all possible changes",
			before: []Package{
				{Name: "gzip", Version: "1.13", Path: "/nix/store/xxx-gzip-1.13"},
				{Name: "gzip", Version: "1.13-lib", Path: "/nix/store/xxx-gzip-1.13-lib"},
				{Name: "gnutar", Version: "1.35-info", Path: "/nix/store/xxx-gnutar-1.35-info"},
			},
			after: []Package{
				{Name: "gzip", Version: "1.14", Path: "/nix/store/xxx-gzip-1.14"},
				{Name: "gzip", Version: "1.14-lib", Path: "/nix/store/xxx-gzip-1.14-lib"},
				{Name: "tar", Version: "1.35-info", Path: "/nix/store/xxx-tar-1.35-info"},
			},
			want: Report{
				Changes: []Change{
					{Name: "gzip", Before: []Version{"1.13", "1.13-lib"}, After: []Version{"1.14", "1.14-lib"}, Type: Changed},
					{Name: "tar", Before: nil, After: []Version{"1.35-info"}, Type: Added},
					{Name: "gnutar", Before: []Version{"1.35-info"}, After: nil, Type: Removed},
				},
				NumBefore: 2,
				NumAfter:  2,
			},
		},
		{
			name: "diff with no changes",
			before: []Package{
				{Name: "gzip", Version: "1.13", Path: "/nix/store/xxx-gzip-1.13"},
				{Name: "gzip", Version: "1.13-lib", Path: "/nix/store/xxx-gzip-1.13-lib"},
				{Name: "gnutar", Version: "1.35-info", Path: "/nix/store/xxx-gnutar-1.35-info"},
			},
			after: []Package{
				{Name: "gzip", Version: "1.13", Path: "/nix/store/xxx-gzip-1.13"},
				{Name: "gzip", Version: "1.13-lib", Path: "/nix/store/xxx-gzip-1.13-lib"},
				{Name: "gnutar", Version: "1.35-info", Path: "/nix/store/xxx-gnutar-1.35-info"},
			},
			want: Report{
				Changes:   nil,
				NumBefore: 2,
				NumAfter:  2,
			},
		},
		{
			name:   "empty before and after",
			before: []Package{},
			after:  []Package{},
			want: Report{
				Changes:   nil,
				NumBefore: 0,
				NumAfter:  0,
			},
		},
		{
			name:   "only additions",
			before: []Package{},
			after: []Package{
				{Name: "gzip", Version: "1.13", Path: "/nix/store/xxx-gzip-1.13"},
			},
			want: Report{
				Changes: []Change{
					{Name: "gzip", Before: nil, After: []Version{"1.13"}, Type: Added},
				},
				NumBefore: 0,
				NumAfter:  1,
			},
		},
		{
			name: "only removals",
			before: []Package{
				{Name: "gzip", Version: "1.13", Path: "/nix/store/xxx-gzip-1.13"},
			},
			after: []Package{},
			want: Report{
				Changes: []Change{
					{Name: "gzip", Before: []Version{"1.13"}, After: nil, Type: Removed},
				},
				NumBefore: 1,
				NumAfter:  0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePackageDiff(tt.before, tt.after)

			if got.NumBefore != tt.want.NumBefore {
				t.Errorf("NumBefore = %d, want %d", got.NumBefore, tt.want.NumBefore)
			}
			if got.NumAfter != tt.want.NumAfter {
				t.Errorf("NumAfter = %d, want %d", got.NumAfter, tt.want.NumAfter)
			}
			if !changesEqual(got.Changes, tt.want.Changes) {
				t.Errorf("Changes mismatch:\ngot:  %v\nwant: %v", got.Changes, tt.want.Changes)
			}
		})
	}
}

func TestSetToSlice(t *testing.T) {
	tests := []struct {
		name string
		in   map[Version]struct{}
		want []Version
	}{
		{
			name: "multiple versions",
			in: map[Version]struct{}{
				"1.13":     {},
				"1.13-lib": {},
			},
			want: []Version{"1.13", "1.13-lib"},
		},
		{
			name: "single version",
			in: map[Version]struct{}{
				"1.35-info": {},
			},
			want: []Version{"1.35-info"},
		},
		{
			name: "empty set",
			in:   map[Version]struct{}{},
			want: []Version{},
		},
		{
			name: "nil set",
			in:   nil,
			want: []Version{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setToSlice(tt.in)

			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Error(diff)
			}
		})
	}
}
