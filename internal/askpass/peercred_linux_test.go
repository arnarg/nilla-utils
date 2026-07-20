//go:build linux

package askpass

import (
	"os"
	"testing"
)

func TestIsDescendant(t *testing.T) {
	pid := os.Getpid()

	// A process is considered to be in its own ancestry (e.g. in-process
	// test connections where the server shares the test binary's PID).
	if !isDescendant(pid, pid) {
		t.Error("expected pid to be accepted as its own descendant")
	}

	// PID 1 (init/systemd) is never a descendant of a test process.
	if isDescendant(1, pid) {
		t.Error("init should not be a descendant of the test process")
	}

	// The parent of the test process is an ancestor, not a descendant.
	if isDescendant(os.Getppid(), pid) {
		t.Error("parent should not be a descendant of the child")
	}

	// A bogus PID that doesn't exist cannot be traced and must be rejected.
	if isDescendant(999999, pid) {
		t.Error("bogus PID should not be a descendant")
	}

	// Invalid PIDs.
	if isDescendant(0, pid) {
		t.Error("PID 0 should not be a descendant")
	}
	if isDescendant(-1, pid) {
		t.Error("negative PID should not be a descendant")
	}
}
