package generation

import (
	"bytes"
	"fmt"
	"os"
	"strings"

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
