package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type statFlavor int

const (
	statGNU statFlavor = iota
	statBSD
)

// sshHost implements Host over an SSH session. The underlying *ssh.Client is
// owned by the host and torn down by Close.
//
// The remote fs operations are implemented with stat/readlink/cat/rm. The SSH
// executor joins command arguments with spaces without shell-quoting, so every
// argument here is kept shell-safe (no spaces or metacharacters). In particular
// the stat format strings use '_' as a field separator (shell-safe) and output
// is parsed positionally: mtime is all digits, the permissions field is exactly
// 10 characters, so the remainder (the path) may itself contain '_'.
type sshHost struct {
	*sshExecutor
	client *ssh.Client
	flavor statFlavor
}

// detectStatFlavor probes the remote stat implementation by trying GNU-style
// arguments first and falling back to BSD-style arguments.
func (h *sshHost) detectStatFlavor(ctx context.Context) (statFlavor, error) {
	if c, err := h.CommandContext(ctx, "stat", "-c", "%Y", "/"); err == nil {
		if c.Run() == nil {
			return statGNU, nil
		}
	}
	if c, err := h.CommandContext(ctx, "stat", "-f", "%m", "/"); err == nil {
		if c.Run() == nil {
			return statBSD, nil
		}
	}
	return 0, fmt.Errorf("could not determine stat flavor on remote host (neither GNU nor BSD stat is available)")
}

// statArgs returns the stat arguments (flags + format) for the detected flavor.
// GNU: -c '%Y_%A_%n'  (mtime, symbolic perms, file name).
// BSD: -f '%m_%Sp_%N' (mtime, symbolic perms, file name).
func statArgs(f statFlavor) []string {
	switch f {
	case statGNU:
		return []string{"-c", "%Y_%A_%n"}
	case statBSD:
		return []string{"-f", "%m_%Sp_%N"}
	}
	return nil
}

func (h *sshHost) Lstat(path string) (EntryInfo, error) {
	c, err := h.Command("stat", append(statArgs(h.flavor), path)...)
	if err != nil {
		return EntryInfo{}, err
	}
	var buf bytes.Buffer
	c.SetStdout(&buf)
	if err := c.Run(); err != nil {
		return EntryInfo{}, notExist(path, err)
	}
	ei, err := parseStatLine(strings.TrimRight(buf.String(), "\n"))
	if err != nil {
		return EntryInfo{}, err
	}
	ei.Name = filepath.Base(path)
	return ei, nil
}

func (h *sshHost) ReadDir(dir string) ([]EntryInfo, error) {
	// Pass "dir/*" and let the remote shell expand the glob. stat fails (exit 1)
	// both when the directory is missing (glob stays literal) and when it is
	// empty; that case is disambiguated with a follow-up directory test.
	c, err := h.Command("stat", append(statArgs(h.flavor), dir+"/*")...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	c.SetStdout(&buf)
	if err := c.Run(); err != nil {
		if exitCode(err) == 1 {
			if ok, _ := h.dirExists(dir); ok {
				return []EntryInfo{}, nil
			}
			return nil, fmt.Errorf("%s: %w", dir, os.ErrNotExist)
		}
		return nil, err
	}

	output := strings.TrimRight(buf.String(), "\n")
	if output == "" {
		return []EntryInfo{}, nil
	}

	lines := strings.Split(output, "\n")
	out := make([]EntryInfo, 0, len(lines))
	for _, line := range lines {
		ei, err := parseStatLine(line)
		if err != nil {
			return nil, err
		}
		out = append(out, ei)
	}
	return out, nil
}

func (h *sshHost) dirExists(dir string) (bool, error) {
	c, err := h.Command("test", "-d", dir)
	if err != nil {
		return false, err
	}
	return c.Run() == nil, nil
}

func (h *sshHost) Readlink(path string) (string, error) {
	c, err := h.Command("readlink", path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	c.SetStdout(&buf)
	if err := c.Run(); err != nil {
		return "", notExist(path, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func (h *sshHost) ReadFile(path string) ([]byte, error) {
	c, err := h.Command("cat", path)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	c.SetStdout(&buf)
	if err := c.Run(); err != nil {
		return nil, notExist(path, err)
	}
	return buf.Bytes(), nil
}

func (h *sshHost) Remove(path string) error {
	c, err := h.Command("rm", path)
	if err != nil {
		return err
	}
	if err := c.Run(); err != nil {
		return notExist(path, err)
	}
	return nil
}

func (h *sshHost) Close() error {
	return h.client.Close()
}

// parseStatLine parses a single "<mtime>_<perms>_<path>" record emitted by the
// flavor-specific stat format. mtime is all digits and perms is exactly 10
// characters, so the split is positional and the path may contain '_'.
func parseStatLine(line string) (EntryInfo, error) {
	mtimeStr, rest, found := strings.Cut(line, "_")
	if !found {
		return EntryInfo{}, fmt.Errorf("malformed stat line: %q", line)
	}
	mtime, err := strconv.ParseInt(mtimeStr, 10, 64)
	if err != nil {
		return EntryInfo{}, fmt.Errorf("malformed stat mtime in %q: %w", line, err)
	}
	if len(rest) < 11 { // 10 perms chars + separator
		return EntryInfo{}, fmt.Errorf("malformed stat line (perms too short): %q", line)
	}
	perms := rest[:10]
	rawName := rest[11:]
	return EntryInfo{
		Name:      filepath.Base(rawName),
		ModTime:   time.Unix(mtime, 0).UTC(),
		IsSymlink: perms[0] == 'l',
	}, nil
}

// notExist maps a "no such file" command failure (exit status 1) to an error
// wrapping os.ErrNotExist; other errors are returned unchanged.
func notExist(path string, err error) error {
	if exitCode(err) == 1 {
		return fmt.Errorf("%s: %w", path, os.ErrNotExist)
	}
	return err
}

func exitCode(err error) int {
	var eerr *ssh.ExitError
	if errors.As(err, &eerr) {
		return eerr.ExitStatus()
	}
	return -1
}
