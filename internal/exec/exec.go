package exec

import (
	"context"
	"errors"
	"io"
	osexec "os/exec"

	"golang.org/x/crypto/ssh"
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
// if the error is not an exit error.
func ExitCode(err error) int {
	var lexec *osexec.ExitError
	if errors.As(err, &lexec) {
		return lexec.ExitCode()
	}
	var sexec *ssh.ExitError
	if errors.As(err, &sexec) {
		return sexec.ExitStatus()
	}
	return -1
}
