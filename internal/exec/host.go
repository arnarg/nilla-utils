package exec

import (
	"context"
	"os"
	"time"

	"github.com/arnarg/nilla-utils/internal/askpass"
)

// EntryInfo is a minimal, eagerly-fetched description of a filesystem entry.
type EntryInfo struct {
	Name      string
	ModTime   time.Time
	IsSymlink bool
}

// Host is an Executor augmented with the filesystem operations needed to
// inspect and manage generations on either the local machine or a remote host.
// Lstat has lstat semantics (it does not follow symlinks).
type Host interface {
	Executor
	Lstat(path string) (EntryInfo, error)
	ReadDir(path string) ([]EntryInfo, error)
	Readlink(path string) (string, error)
	ReadFile(path string) ([]byte, error)
	Remove(path string) error
	Close() error
}

// NewHost returns a Host backed by the local filesystem when target is empty,
// or an SSH-backed Host connected to target otherwise. For remote targets it
// owns the *ssh.Client connection (and a fresh password cache when cache is
// nil). Close tears it down.
func NewHost(ctx context.Context, target string, cache *askpass.PasswordCache) (Host, error) {
	if target == "" {
		return &localHost{Executor: NewLocalExecutor()}, nil
	}

	if cache == nil {
		cache = askpass.NewPasswordCache()
	}

	client, err := dialSSH(target, cache)
	if err != nil {
		return nil, err
	}

	h := &sshHost{sshExecutor: &sshExecutor{client: client}, client: client}

	flavor, err := h.detectStatFlavor(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}
	h.flavor = flavor

	return h, nil
}

// localHost implements Host using the local filesystem.
type localHost struct {
	Executor
}

func (h *localHost) Lstat(path string) (EntryInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return EntryInfo{}, err
	}
	return EntryInfo{
		Name:      info.Name(),
		ModTime:   info.ModTime(),
		IsSymlink: info.Mode()&os.ModeSymlink != 0,
	}, nil
}

func (h *localHost) ReadDir(path string) ([]EntryInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	out := make([]EntryInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, EntryInfo{
			Name:      e.Name(),
			ModTime:   info.ModTime(),
			IsSymlink: e.Type()&os.ModeSymlink != 0,
		})
	}
	return out, nil
}

func (h *localHost) Readlink(path string) (string, error) {
	return os.Readlink(path)
}

func (h *localHost) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (h *localHost) Remove(path string) error {
	return os.Remove(path)
}

func (h *localHost) Close() error {
	return nil
}
