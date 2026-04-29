package exec

import (
	"context"
	"io"
	"os"
	"os/exec"
)

type localExecutor struct {
	env []string
}

func NewLocalExecutor(extraEnv ...string) Executor {
	return &localExecutor{env: extraEnv}
}

func (e *localExecutor) Command(cmd string, args ...string) (Command, error) {
	return &localCommand{Cmd: exec.Command(cmd, args...), extraEnv: e.env}, nil
}

func (e *localExecutor) CommandContext(ctx context.Context, cmd string, args ...string) (Command, error) {
	return &localCommand{Cmd: exec.CommandContext(ctx, cmd, args...), extraEnv: e.env}, nil
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
	extraEnv []string
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

func (c *localCommand) Start() error {
	if len(c.extraEnv) > 0 {
		c.Cmd.Env = append(os.Environ(), c.extraEnv...)
	}
	return c.Cmd.Start()
}

func (c *localCommand) Run() error {
	if len(c.extraEnv) > 0 {
		c.Cmd.Env = append(os.Environ(), c.extraEnv...)
	}
	return c.Cmd.Run()
}

func NewAskpassExec(socketPath, commandID string) Executor {
	self, _ := os.Executable()
	return NewLocalExecutor(
		"SSH_ASKPASS="+self,
		"SSH_ASKPASS_REQUIRE=force",
		"NILLA_ASKPASS_SOCKET="+socketPath,
		"NILLA_ASKPASS_COMMAND_ID="+commandID,
	)
}
