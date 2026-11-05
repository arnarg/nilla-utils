package exec

import (
	"context"
	"errors"
	"io"
	osexec "os/exec"
)

type Executor interface {
	Command(string, ...string) (Command, error)
	CommandContext(context.Context, string, ...string) (Command, error)
	PathExists(string) (bool, error)
	IsLocal() bool
}

type Command interface {
	Run() error
	Start() error
	Wait() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.Reader, error)
	StderrPipe() (io.Reader, error)
}

// ExitCode returns the exit status of a command run locally or over SSH, or -1
// if the error is not an exit error. Over SSH, 255 can also mean that ssh
// itself failed.
func ExitCode(err error) int {
	var eerr *osexec.ExitError
	if errors.As(err, &eerr) {
		return eerr.ExitCode()
	}
	return -1
}
