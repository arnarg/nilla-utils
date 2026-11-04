package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchUpForGitDir(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "regular repository with .git directory",
			setup: func(t *testing.T, root string) {
				if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "worktree with .git file",
			setup: func(t *testing.T, root string) {
				content := []byte("gitdir: /some/repo/.git/worktrees/wt\n")
				if err := os.WriteFile(filepath.Join(root, ".git"), content, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.setup(t, root)

			nested := filepath.Join(root, "sub", "dir")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatal(err)
			}

			for _, start := range []string{root, nested} {
				got, err := searchUpForGitDir(start)
				if err != nil {
					t.Fatalf("searchUpForGitDir(%q) returned error: %v", start, err)
				}
				if got != root {
					t.Errorf("searchUpForGitDir(%q) = %q, want %q", start, got, root)
				}
			}
		})
	}
}
