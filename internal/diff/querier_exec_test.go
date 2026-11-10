package diff

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/arnarg/nilla-utils/internal/exec"
)

// recordingExecutor records the commands it is asked to run and writes
// stdout to each of them.
type recordingExecutor struct {
	stdout string
	ran    [][]string
}

func (e *recordingExecutor) Command(name string, args ...string) (exec.Command, error) {
	e.ran = append(e.ran, append([]string{name}, args...))
	return &recordedCommand{stdout: e.stdout}, nil
}

func (e *recordingExecutor) CommandContext(_ context.Context, name string, args ...string) (exec.Command, error) {
	return e.Command(name, args...)
}

func (e *recordingExecutor) PathExists(string) (bool, error) { return false, nil }
func (e *recordingExecutor) IsLocal() bool                   { return false }

type recordedCommand struct {
	stdout string
	out    io.Writer
}

func (c *recordedCommand) Run() error {
	if c.out != nil {
		_, err := io.WriteString(c.out, c.stdout)
		return err
	}
	return nil
}
func (c *recordedCommand) Start() error                       { return nil }
func (c *recordedCommand) Wait() error                        { return c.Run() }
func (c *recordedCommand) SetStdin(io.Reader)                 {}
func (c *recordedCommand) SetStdout(w io.Writer)              { c.out = w }
func (c *recordedCommand) SetStderr(io.Writer)                {}
func (c *recordedCommand) StdinPipe() (io.WriteCloser, error) { return nil, nil }
func (c *recordedCommand) StdoutPipe() (io.Reader, error)     { return nil, nil }
func (c *recordedCommand) StderrPipe() (io.Reader, error)     { return nil, nil }

// nix-command is off by default, e.g. on a freshly installed target, so
// path-info must enable it itself.
func TestGetClosureSizeEnablesNixCommand(t *testing.T) {
	e := &recordingExecutor{stdout: `[{"closureSize":1234}]`}
	size, err := NewExecutorQuerier(e).GetClosureSize(context.Background(), "/nix/store/abc-system")
	if err != nil {
		t.Fatal(err)
	}
	if size != 1234 {
		t.Errorf("size = %d, want 1234", size)
	}

	want := []string{"nix", "--extra-experimental-features", "nix-command", "path-info", "--json", "--closure-size", "/nix/store/abc-system"}
	if len(e.ran) != 1 || !slices.Equal(e.ran[0], want) {
		t.Errorf("ran %q, want %q", e.ran, want)
	}
}
