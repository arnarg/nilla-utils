package diff

import (
	"testing"
)

func TestLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		name     string
		before   []Version
		after    []Version
		expected int
	}{
		{
			name:     "simple patch version bump",
			before:   []Version{"1.2.3"},
			after:    []Version{"1.2.4"},
			expected: 4,
		},
		{
			name:     "simple minor version bump",
			before:   []Version{"1.2.0"},
			after:    []Version{"1.3.0"},
			expected: 2,
		},
		{
			name:     "simple major version bump",
			before:   []Version{"1.2.0"},
			after:    []Version{"2.1.0"},
			expected: 0,
		},
		{
			name:     "version containing dash",
			before:   []Version{"2.4.11-lib"},
			after:    []Version{"2.4.11-man"},
			expected: 7,
		},
		{
			name:     "multiple versions mixed",
			before:   []Version{"2.4.11", "2.4.11-man"},
			after:    []Version{"2.5", "2.5-man", "2.5-lib"},
			expected: 2,
		},
		{
			name:     "multiple versions mixed unchanged",
			before:   []Version{"2.4.11", "2.4.11-man"},
			after:    []Version{"2.4.11", "2.4.11-man"},
			expected: 4,
		},
		{
			name:     "multiple versions with empty ones",
			before:   []Version{"", "257.7"},
			after:    []Version{"", "257.8"},
			expected: 4,
		},
		{
			name:     "only empty version strings",
			before:   []Version{""},
			after:    []Version{""},
			expected: 0,
		},
		{
			name:     "only actual version removed",
			before:   []Version{"", "257.7"},
			after:    []Version{"", ""},
			expected: 0,
		},
		{
			name:     "empty before",
			before:   []Version{},
			after:    []Version{"1.0.0"},
			expected: 0,
		},
		{
			name:     "empty after",
			before:   []Version{"1.0.0"},
			after:    []Version{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := longestCommonPrefix(tt.before, tt.after)

			if tt.expected != result {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestVersionsToString(t *testing.T) {
	tests := []struct {
		name string
		in   []Version
		want string
	}{
		{
			name: "multiple versions",
			in:   []Version{"1.13", "1.13-lib"},
			want: "1.13, 1.13-lib",
		},
		{
			name: "single version",
			in:   []Version{"1.35-info"},
			want: "1.35-info",
		},
		{
			name: "empty slice",
			in:   []Version{},
			want: "",
		},
		{
			name: "nil slice",
			in:   nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionsToString(tt.in)

			if got != tt.want {
				t.Errorf("versionsToString() = %q, want %q", got, tt.want)
			}
		})
	}
}
