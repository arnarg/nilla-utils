package generation

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/util"
)

// homeDirResolver provides a way to get a user's home directory
type homeDirResolver interface {
	getHomeDir(user string) (string, error)
}

// linkReader provides a way to read symlinks
type linkReader interface {
	readLink(path string) (string, error)
	pathExists(path string) (bool, error)
}

// dirReader provides a way to read directories
type dirReader interface {
	readDir(path string) ([]fs.DirEntry, error)
	stat(path string) (fs.FileInfo, error)
}

// fileReader provides a way to read files
type fileReader interface {
	readFile(path string) ([]byte, error)
}

// fileDeleter provides a way to delete files
type fileDeleter interface {
	delete(path string) error
}

// Local implementations

type localHomeDirResolver struct{}

func (r *localHomeDirResolver) getHomeDir(user string) (string, error) {
	return util.GetHomeDir(), nil
}

type localLinkReader struct{}

func (r *localLinkReader) readLink(path string) (string, error) {
	return os.Readlink(path)
}

func (r *localLinkReader) pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

type localDirReader struct{}

func (r *localDirReader) readDir(path string) ([]fs.DirEntry, error) {
	return os.ReadDir(path)
}

func (r *localDirReader) stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

type localFileReader struct{}

func (r *localFileReader) readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

type localFileDeleter struct{}

func (r *localFileDeleter) delete(path string) error {
	return os.Remove(path)
}

// Remote implementations

type remoteHomeDirResolver struct {
	executor exec.Executor
}

func (r *remoteHomeDirResolver) getHomeDir(user string) (string, error) {
	// Try getent first (if available), then fall back to eval ~user
	// Check if getent exists before trying it
	checkCmd, err := r.executor.Command("command", "-v", "getent")
	if err == nil {
		var checkBuf bytes.Buffer
		checkCmd.SetStdout(&checkBuf)
		if checkCmd.Run() == nil && strings.TrimSpace(checkBuf.String()) != "" {
			// getent exists, try using it
			getentCmd, err := r.executor.Command("getent", "passwd", user)
			if err == nil {
				var getentBuf bytes.Buffer
				getentCmd.SetStdout(&getentBuf)
				if err := getentCmd.Run(); err == nil {
					fields := strings.Split(strings.TrimSpace(getentBuf.String()), ":")
					if len(fields) >= 6 {
						return fields[5], nil
					}
				}
			}
		}
	}

	// Fallback to eval ~user (works on both Linux and Darwin)
	evalCmd, err := r.executor.Command("sh", "-c", fmt.Sprintf("eval echo ~%s", user))
	if err != nil {
		return "", err
	}
	var homeDirBuf bytes.Buffer
	evalCmd.SetStdout(&homeDirBuf)
	if err := evalCmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(homeDirBuf.String()), nil
}

type remoteLinkReader struct {
	executor exec.Executor
}

func (r *remoteLinkReader) readLink(path string) (string, error) {
	cmd, err := r.executor.Command("readlink", path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	cmd.SetStdout(&buf)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func (r *remoteLinkReader) pathExists(path string) (bool, error) {
	return r.executor.PathExists(path)
}

type remoteDirReader struct {
	executor exec.Executor
}

func (r *remoteDirReader) readDir(path string) ([]fs.DirEntry, error) {
	// Use ls -1 to list directory entries
	cmd, err := r.executor.Command("ls", "-1", path)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	cmd.SetStdout(&buf)
	if err := cmd.Run(); err != nil {
		// If directory doesn't exist, return empty list
		return []fs.DirEntry{}, nil
	}

	// Parse ls output and create entries
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	entries := []fs.DirEntry{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Build full path
		fullPath := filepath.Join(path, line)
		// Create a simple DirEntry wrapper
		entries = append(entries, &dirEntryWrapper{name: line, path: fullPath, executor: r.executor})
	}
	return entries, nil
}

// getStatCommand returns the appropriate stat command and arguments for the platform.
// Linux uses: stat -c "%Y" path
// macOS/BSD uses: stat -f "%m" path
func getStatCommand(path string) (string, []string) {
	// Try to detect platform, default to Linux
	if runtime.GOOS == "darwin" {
		return "stat", []string{"-f", "%m", path}
	}
	// Default to Linux format
	return "stat", []string{"-c", "%Y", path}
}

func (r *remoteDirReader) stat(path string) (fs.FileInfo, error) {
	// Use stat command via executor to get modtime
	cmdName, args := getStatCommand(path)
	statCmd, err := r.executor.Command(cmdName, args...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	statCmd.SetStdout(&buf)
	var modTime time.Time
	if err := statCmd.Run(); err == nil {
		if timestamp, err := strconv.ParseInt(strings.TrimSpace(buf.String()), 10, 64); err == nil {
			modTime = time.Unix(timestamp, 0)
		}
	}
	if modTime.IsZero() {
		modTime = time.Now()
	}

	// Create a fileInfo wrapper
	return &fileInfoWrapper{
		name:    filepath.Base(path),
		path:    path,
		modTime: modTime,
	}, nil
}

// dirEntryWrapper wraps a remote directory entry
type dirEntryWrapper struct {
	name     string
	path     string
	executor exec.Executor
}

func (e *dirEntryWrapper) Name() string {
	return e.name
}

func (e *dirEntryWrapper) IsDir() bool {
	return false // generations are symlinks
}

func (e *dirEntryWrapper) Type() fs.FileMode {
	return fs.ModeSymlink
}

func (e *dirEntryWrapper) Info() (fs.FileInfo, error) {
	// Use stat command via executor to get modtime
	cmdName, args := getStatCommand(e.path)
	statCmd, err := e.executor.Command(cmdName, args...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	statCmd.SetStdout(&buf)
	var modTime time.Time
	if err := statCmd.Run(); err == nil {
		if timestamp, err := strconv.ParseInt(strings.TrimSpace(buf.String()), 10, 64); err == nil {
			modTime = time.Unix(timestamp, 0)
		}
	}
	if modTime.IsZero() {
		modTime = time.Now()
	}
	return &fileInfoWrapper{
		name:    e.name,
		path:    e.path,
		modTime: modTime,
	}, nil
}

// fileInfoWrapper wraps remote file info
type fileInfoWrapper struct {
	name    string
	path    string
	modTime time.Time
	size    int64
}

func (f *fileInfoWrapper) Name() string {
	return f.name
}

func (f *fileInfoWrapper) Size() int64 {
	return f.size
}

func (f *fileInfoWrapper) Mode() fs.FileMode {
	return fs.ModeSymlink
}

func (f *fileInfoWrapper) ModTime() time.Time {
	return f.modTime
}

func (f *fileInfoWrapper) IsDir() bool {
	return false
}

func (f *fileInfoWrapper) Sys() interface{} {
	return nil
}

type remoteFileReader struct {
	executor exec.Executor
}

func (r *remoteFileReader) readFile(path string) ([]byte, error) {
	cmd, err := r.executor.Command("cat", path)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	cmd.SetStdout(&buf)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type remoteFileDeleter struct {
	executor exec.Executor
}

func (r *remoteFileDeleter) delete(path string) error {
	// Check if sudo is needed:
	// - NixOS system profiles: always need sudo
	// - Home Manager root user profiles: need sudo
	// - Home Manager regular user profiles: no sudo needed
	needsSudo := strings.Contains(path, "/nix/var/nix/profiles/system-") ||
		strings.Contains(path, "/nix/var/nix/profiles/per-user/root/")

	var cmd exec.Command
	var err error
	if needsSudo {
		cmd, err = exec.CommandWithSudoIfNeeded(r.executor, "rm", path)
	} else {
		cmd, err = r.executor.Command("rm", path)
	}
	if err != nil {
		return err
	}

	// Set stdin/stderr/stdout for sudo password prompts
	if needsSudo {
		cmd.SetStdin(os.Stdin)
		cmd.SetStderr(os.Stderr)
		cmd.SetStdout(os.Stdout)
	}

	return cmd.Run()
}

// Factory functions

// createReaders creates the appropriate reader implementations based on the executor.
func createReaders(e exec.Executor) (linkReader, dirReader, fileReader) {
	if e == nil || e.IsLocal() {
		return &localLinkReader{}, &localDirReader{}, &localFileReader{}
	}
	return &remoteLinkReader{executor: e}, &remoteDirReader{executor: e}, &remoteFileReader{executor: e}
}

// createDeleter creates the appropriate deleter implementation based on the executor.
func createDeleter(e exec.Executor) fileDeleter {
	if e == nil || e.IsLocal() {
		return &localFileDeleter{}
	}
	return &remoteFileDeleter{executor: e}
}

// createHomeDirResolver creates the appropriate home directory resolver based on the executor.
func createHomeDirResolver(e exec.Executor) homeDirResolver {
	if e == nil || e.IsLocal() {
		return &localHomeDirResolver{}
	}
	return &remoteHomeDirResolver{executor: e}
}
