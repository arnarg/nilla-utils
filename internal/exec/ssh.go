package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"charm.land/log/v2"
	"golang.org/x/term"
)

// sshExecutor runs commands on a remote host over a shared OpenSSH
// connection. Close releases the connection.
type sshExecutor struct {
	conn *sshConn
}

// NewSSHExecutor connects to target. The returned executor implements
// io.Closer; close it when done to shut the connection down.
func NewSSHExecutor(ctx context.Context, target string) (Executor, error) {
	conn, err := dialSSH(ctx, target)
	if err != nil {
		return nil, err
	}
	return &sshExecutor{conn: conn}, nil
}

func (e *sshExecutor) Close() error {
	return e.conn.Close()
}

func (e *sshExecutor) Command(cmd string, args ...string) (Command, error) {
	return e.CommandContext(context.Background(), cmd, args...)
}

func (e *sshExecutor) CommandContext(ctx context.Context, cmd string, args ...string) (Command, error) {
	remote := buildRemoteCmd(cmd, args)
	log.Debugf("Running on %s: %s", e.conn.target, remote)

	c := osexec.CommandContext(ctx, sshBinary)
	// On cancellation, ask ssh to exit rather than killing it, so it can
	// restore the terminal and close the session cleanly. A remote command
	// with a tty then gets SIGHUP; one without stops when it next writes
	// output (OpenSSH's client cannot send signals).
	c.Cancel = func() error { return c.Process.Signal(syscall.SIGTERM) }
	c.WaitDelay = 5 * time.Second

	return &sshCommand{
		Cmd:    c,
		conn:   e.conn,
		remote: remote,
		sudo:   cmd == "sudo",
	}, nil
}

func (e *sshExecutor) PathExists(path string) (bool, error) {
	cmd, err := e.Command("test", "-e", path)
	if err != nil {
		return false, err
	}

	if err := cmd.Run(); err != nil {
		if ExitCode(err) == 1 {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (e *sshExecutor) IsLocal() bool {
	return false
}

// safeRemoteChars matches arguments that can be passed through the remote
// login shell unquoted. Note that '*' is included on purpose: ReadDir passes
// "dir/*" and relies on the remote shell expanding the glob.
var safeRemoteChars = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./\*-]+$`)

// shellQuote quotes an argument for safe passage through the remote login
// shell, which re-splits the command string sent over the SSH session.
func shellQuote(arg string) string {
	if safeRemoteChars.MatchString(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func buildRemoteCmd(cmd string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(cmd))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// sshCommand is a local `ssh` process running one remote command.
type sshCommand struct {
	*osexec.Cmd
	conn   *sshConn
	remote string
	sudo   bool
}

func (c *sshCommand) SetStdin(r io.Reader)  { c.Cmd.Stdin = r }
func (c *sshCommand) SetStdout(w io.Writer) { c.Cmd.Stdout = w }
func (c *sshCommand) SetStderr(w io.Writer) { c.Cmd.Stderr = w }

func (c *sshCommand) StdoutPipe() (io.Reader, error) { return c.Cmd.StdoutPipe() }
func (c *sshCommand) StderrPipe() (io.Reader, error) { return c.Cmd.StderrPipe() }

// wantsTTY reports whether to allocate a remote tty: for sudo when our
// stdin is a terminal, so sudo can prompt for the password and Ctrl-C
// reaches the remote command.
func (c *sshCommand) wantsTTY() bool {
	if !c.sudo {
		return false
	}
	f, ok := c.Cmd.Stdin.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (c *sshCommand) Start() error {
	c.Cmd.Args = append([]string{sshBinary}, c.conn.commandArgs(c.remote, c.wantsTTY())...)
	return c.Cmd.Start()
}

func (c *sshCommand) Wait() error {
	return c.wrapErr(c.Cmd.Wait())
}

func (c *sshCommand) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// wrapErr explains exit status 255, which ssh uses for its own failures,
// when the shared connection has gone away. The original error stays
// wrapped so ExitCode still works.
func (c *sshCommand) wrapErr(err error) error {
	var eerr *osexec.ExitError
	if errors.As(err, &eerr) && eerr.ExitCode() == 255 && c.conn.lost() {
		return fmt.Errorf("lost SSH connection to %s: %w", c.conn.target, err)
	}
	return err
}

func parseTarget(target string) (user string, host string, port string) {
	// Split on @
	parts := strings.SplitN(target, "@", 2)

	// If there's 2 parts, we have a user@host
	if len(parts) == 2 {
		user = parts[0]
		host = parts[1]
	} else {
		host = parts[0]
	}

	// IPv6 addresses are written as [addr] or [addr]:port
	if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end > 0 {
			addr, rest := host[1:end], host[end+1:]
			host = addr
			port = strings.TrimPrefix(rest, ":")
			return
		}
	}

	// Check if port is specified
	parts = strings.SplitN(host, ":", 2)

	// If there's 2 parts, we have host:port
	if len(parts) == 2 {
		host = parts[0]
		port = parts[1]
	}

	return
}
