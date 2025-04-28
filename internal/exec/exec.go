package exec

import (
	"context"
	"io"
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
