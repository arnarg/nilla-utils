package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseStatLine_GNU(t *testing.T) {
	// GNU stat -c '%Y_%A_%n' output: mtime, 10-char perms, name (full path).
	tests := []struct {
		name      string
		line      string
		wantName  string
		wantLink  bool
		wantEpoch int64
	}{
		{"symlink", "1700000000_lrwxrwxrwx_/nix/var/nix/profiles/system-5-link", "system-5-link", true, 1700000000},
		{"regular", "1699000000_-rw-r--r--_/nix/var/nix/profiles/system-1-link", "system-1-link", false, 1699000000},
		{"directory", "1680000000_drwxr-xr-x_/nix/var/nix/profiles/per-user", "per-user", false, 1680000000},
		{"path with underscore", "1700000000_lrwxrwxrwx_/nix/var/nix/profiles/system_2_link", "system_2_link", true, 1700000000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ei, err := parseStatLine(tt.line)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ei.Name != tt.wantName {
				t.Errorf("name: got %q want %q", ei.Name, tt.wantName)
			}
			if ei.IsSymlink != tt.wantLink {
				t.Errorf("symlink: got %v want %v", ei.IsSymlink, tt.wantLink)
			}
			if want := time.Unix(tt.wantEpoch, 0).UTC(); !ei.ModTime.Equal(want) {
				t.Errorf("mtime: got %v want %v", ei.ModTime, want)
			}
		})
	}
}

func TestParseStatLine_BSD(t *testing.T) {
	// BSD stat -f '%m_%Sp_%N' output has the same positional shape.
	tests := []struct {
		name     string
		line     string
		wantName string
		wantLink bool
	}{
		{"symlink", "1700000000_lrwxr-xr-x_/nix/var/nix/profiles/system-5-link", "system-5-link", true},
		{"regular", "1699000000_-rw-r--r--_/nix/var/nix/profiles/system-1-link", "system-1-link", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ei, err := parseStatLine(tt.line)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ei.Name != tt.wantName {
				t.Errorf("name: got %q want %q", ei.Name, tt.wantName)
			}
			if ei.IsSymlink != tt.wantLink {
				t.Errorf("symlink: got %v want %v", ei.IsSymlink, tt.wantLink)
			}
		})
	}
}

func TestParseStatLine_Errors(t *testing.T) {
	cases := []string{
		"",                     // empty
		"1700000000",           // no separator
		"abc_lrwxrwxrwx_/foo",  // mtime not numeric
		"1700000000_lrwx_/foo", // perms too short
	}
	for _, c := range cases {
		if _, err := parseStatLine(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

// TestLocalHost validates the local Host against real filesystem state using
// os.* as the oracle.
func TestLocalHost(t *testing.T) {
	dir := t.TempDir()

	// regular file
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	// symlink -> file
	if err := os.Symlink("file.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	h, err := NewHost(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	defer h.Close()

	if !h.IsLocal() {
		t.Fatalf("expected local host")
	}

	t.Run("ReadDir", func(t *testing.T) {
		entries, err := h.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		byName := map[string]EntryInfo{}
		for _, e := range entries {
			byName[e.Name] = e
		}
		if _, ok := byName["file.txt"]; !ok {
			t.Errorf("missing file.txt: %+v", byName)
		}
		link, ok := byName["link"]
		if !ok {
			t.Fatalf("missing link: %+v", byName)
		}
		if !link.IsSymlink {
			t.Errorf("link not flagged symlink: %+v", link)
		}
	})

	t.Run("Lstat symlink does not follow", func(t *testing.T) {
		ei, err := h.Lstat(filepath.Join(dir, "link"))
		if err != nil {
			t.Fatalf("Lstat: %v", err)
		}
		if !ei.IsSymlink {
			t.Errorf("Lstat should not follow symlink: %+v", ei)
		}
	})

	t.Run("Readlink", func(t *testing.T) {
		target, err := h.Readlink(filepath.Join(dir, "link"))
		if err != nil {
			t.Fatalf("Readlink: %v", err)
		}
		if target != "file.txt" {
			t.Errorf("Readlink: got %q want file.txt", target)
		}
	})

	t.Run("ReadFile", func(t *testing.T) {
		b, err := h.ReadFile(filepath.Join(dir, "file.txt"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(b) != "hello" {
			t.Errorf("ReadFile: got %q want hello", b)
		}
	})

	t.Run("ReadDir missing", func(t *testing.T) {
		_, err := h.ReadDir(filepath.Join(dir, "nope"))
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected ErrNotExist, got %v", err)
		}
	})

	t.Run("Lstat missing", func(t *testing.T) {
		_, err := h.Lstat(filepath.Join(dir, "nope"))
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected ErrNotExist, got %v", err)
		}
	})

	t.Run("ReadFile missing", func(t *testing.T) {
		_, err := h.ReadFile(filepath.Join(dir, "nope"))
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected ErrNotExist, got %v", err)
		}
	})

	t.Run("Remove", func(t *testing.T) {
		p := filepath.Join(dir, "gone.txt")
		os.WriteFile(p, []byte("x"), 0644)
		if err := h.Remove(p); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if _, err := os.Stat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected gone, got %v", err)
		}
	})

	t.Run("Remove missing -> ErrNotExist", func(t *testing.T) {
		if err := h.Remove(filepath.Join(dir, "ghost")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected ErrNotExist, got %v", err)
		}
	})
}

// TestNewHostLocal constructs a local host and asserts it satisfies Host.
func TestNewHostLocal(t *testing.T) {
	h, err := NewHost(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	defer h.Close()
	var _ Host = h
}

// TestLocalHostCommandPassthrough verifies the embedded Executor still runs
// commands locally.
func TestLocalHostCommandPassthrough(t *testing.T) {
	h, err := NewHost(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	defer h.Close()

	c, err := h.CommandContext(context.Background(), "printf", "%s", "ok")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	var buf bytes.Buffer
	c.SetStdout(&buf)
	if err := c.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if buf.String() != "ok" {
		t.Errorf("got %q want ok", buf.String())
	}
}

// TestStatArgs sanity-checks the per-flavor argument shape used by the remote
// host. This guards against accidental format regressions.
func TestStatArgs(t *testing.T) {
	gnu := statArgs(statGNU)
	if fmt.Sprint(gnu) != "[-c %Y_%A_%n]" {
		t.Errorf("GNU stat args: %v", gnu)
	}
	bsd := statArgs(statBSD)
	if fmt.Sprint(bsd) != "[-f %m_%Sp_%N]" {
		t.Errorf("BSD stat args: %v", bsd)
	}
}
