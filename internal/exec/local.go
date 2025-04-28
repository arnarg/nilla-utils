package exec

import (
	"context"
	"io"
	"os"
	"os/exec"
)

type localExecutor struct{}

func NewLocalExecutor() Executor {
	return &localExecutor{}
}

func (e *localExecutor) Command(cmd string, args ...string) (Command, error) {
	return &localCommand{exec.Command(cmd, args...)}, nil
}

func (e *localExecutor) CommandContext(ctx context.Context, cmd string, args ...string) (Command, error) {
	return &localCommand{exec.CommandContext(ctx, cmd, args...)}, nil
}

func (e *localExecutor) PathExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (e *localExecutor) IsLocal() bool {
	return true
}

type localCommand struct {
	*exec.Cmd
}

func (c *localCommand) SetStdin(r io.Reader) {
	c.Cmd.Stdin = r
}

func (c *localCommand) SetStdout(w io.Writer) {
	c.Cmd.Stdout = w
}

func (c *localCommand) SetStderr(w io.Writer) {
	c.Cmd.Stderr = w
}

func (c *localCommand) StdoutPipe() (io.Reader, error) {
	return c.Cmd.StdoutPipe()
}

func (c *localCommand) StderrPipe() (io.Reader, error) {
	return c.Cmd.StderrPipe()
}
