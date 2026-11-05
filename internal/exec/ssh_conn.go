package exec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/util"
)

// Remote hosts are reached through OpenSSH's connection multiplexing. For
// each host nilla starts one master connection (ssh -fNM) up front, before
// any progress UI runs, so OpenSSH does all prompting (passwords, key
// passphrases, unknown host keys) on the terminal exactly as `ssh` would.
// Every later ssh process, both nilla's own commands and the ones nix starts
// for `nix copy` and remote builds (via NIX_SSHOPTS), reuses that connection
// without authenticating again.
//
// Lifetime: the master is started with a short ControlPersist, so it exits
// shortly after its last client disconnects. A "keeper" session runs `cat`
// on the remote host with its stdin connected to a pipe held by nilla. While
// nilla runs, the keeper keeps the master alive however long a local build
// takes; if nilla exits for any reason, even a crash, the pipe closes, the
// keeper ends and the master shuts down ControlPersist later. Close shuts it
// down immediately with `ssh -O exit`.

var (
	// sshBinary is the OpenSSH client used for all connections.
	sshBinary = "ssh"
	// sshExtraArgs are passed to every ssh invocation, including the ones
	// nix makes. Tests use it to point ssh at a private config (-F).
	sshExtraArgs []string
	// controlPersist is how long a master outlives its last client.
	controlPersist = "10"
	// keeperTimeout bounds waiting for the keeper session to start.
	keeperTimeout = 30 * time.Second
)

// muxRegistry tracks the control socket directory and the masters running
// in it. Masters are keyed by their expanded control socket path, so two
// targets that OpenSSH resolves to the same user, host and port (e.g. "host"
// and "user@host") share one master.
var muxRegistry = struct {
	sync.Mutex
	dir     string
	masters map[string]*sshMaster
}{masters: map[string]*sshMaster{}}

type sshMaster struct {
	socket   string
	hostArgs []string
	refs     int

	keeperW    *os.File
	keeper     *osexec.Cmd
	keeperDone chan struct{}
}

// sshConn is a reference to a shared master connection.
type sshConn struct {
	target   string
	hostArgs []string
	master   *sshMaster

	closeOnce sync.Once
}

// controlPathOpt returns the ControlPath option for the shared directory.
func controlPathOpt(dir string) string {
	return "ControlPath=" + filepath.Join(dir, "%C")
}

// muxClientArgs are the options for ssh processes that should use an
// existing master and never authenticate (or prompt) on their own.
func muxClientArgs(dir string) []string {
	return []string{"-o", controlPathOpt(dir), "-o", "ControlMaster=no", "-o", "BatchMode=yes"}
}

// NixSSHOptsEnv returns a NIX_SSHOPTS=... entry that makes the ssh processes
// started by nix reuse nilla's open connections, or "" if there are none.
// Options already in NIX_SSHOPTS are kept, after nilla's (ssh uses the first
// value it sees for an option).
func NixSSHOptsEnv() string {
	muxRegistry.Lock()
	defer muxRegistry.Unlock()

	if len(muxRegistry.masters) == 0 {
		return ""
	}

	opts := append(muxClientArgs(muxRegistry.dir), sshExtraArgs...)
	if existing := strings.TrimSpace(os.Getenv("NIX_SSHOPTS")); existing != "" {
		opts = append(opts, existing)
	}
	return "NIX_SSHOPTS=" + strings.Join(opts, " ")
}

// dialSSH opens (or reuses) a shared connection to target, which is
// [user@]host[:port] or a host alias from ssh_config.
func dialSSH(ctx context.Context, target string) (*sshConn, error) {
	if _, err := osexec.LookPath(sshBinary); err != nil {
		return nil, fmt.Errorf("connecting to remote hosts requires the OpenSSH client (ssh) in PATH: %w", err)
	}

	user, host, port := parseTarget(target)
	if host == "" {
		return nil, fmt.Errorf("invalid SSH target %q", target)
	}
	hostArgs := []string{}
	if user != "" {
		hostArgs = append(hostArgs, "-l", user)
	}
	if port != "" {
		hostArgs = append(hostArgs, "-p", port)
	}
	hostArgs = append(hostArgs, "--", host)

	// Dials are serialized: masters are shared, and only one prompt should
	// be on the terminal at a time anyway.
	muxRegistry.Lock()
	defer muxRegistry.Unlock()

	dir, err := ensureControlDir()
	if err != nil {
		return nil, err
	}

	socket, err := resolveControlSocket(ctx, dir, hostArgs)
	if err != nil {
		cleanupControlDirLocked()
		return nil, err
	}

	if m, ok := muxRegistry.masters[socket]; ok {
		m.refs++
		return &sshConn{target: target, hostArgs: hostArgs, master: m}, nil
	}

	m := &sshMaster{socket: socket, hostArgs: hostArgs, refs: 1}
	if err := m.start(ctx, dir, target); err != nil {
		cleanupControlDirLocked()
		return nil, err
	}
	muxRegistry.masters[socket] = m

	return &sshConn{target: target, hostArgs: hostArgs, master: m}, nil
}

// ensureControlDir creates the private directory holding control sockets.
// Unix socket paths are limited to ~104 bytes, so it lives in
// XDG_RUNTIME_DIR or /tmp rather than a possibly long $TMPDIR. The path
// must not contain whitespace since it ends up in NIX_SSHOPTS, which nix
// splits on whitespace.
func ensureControlDir() (string, error) {
	if muxRegistry.dir != "" {
		return muxRegistry.dir, nil
	}

	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" || strings.ContainsAny(base, " \t\n%") || len(base) > 40 {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "nilla-ssh-")
	if err != nil {
		return "", fmt.Errorf("creating SSH control socket directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	muxRegistry.dir = dir
	return dir, nil
}

func cleanupControlDirLocked() {
	if len(muxRegistry.masters) == 0 && muxRegistry.dir != "" {
		os.RemoveAll(muxRegistry.dir)
		muxRegistry.dir = ""
	}
}

// resolveControlSocket asks OpenSSH which control socket it will use for
// the host, i.e. the expanded %C (a hash of local host, remote user, host,
// port and ProxyJump after ssh_config is applied).
func resolveControlSocket(ctx context.Context, dir string, hostArgs []string) (string, error) {
	args := append(append([]string{}, sshExtraArgs...), "-G", "-o", controlPathOpt(dir))
	args = append(args, hostArgs...)

	var stdout, stderr bytes.Buffer
	cmd := osexec.CommandContext(ctx, sshBinary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("evaluating ssh config for %s: %s", hostArgs[len(hostArgs)-1], msg)
	}

	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		if path, ok := strings.CutPrefix(scanner.Text(), "controlpath "); ok {
			return path, nil
		}
	}
	return "", errors.New("ssh -G did not report a control path")
}

func (m *sshMaster) baseArgs(opts ...string) []string {
	args := append([]string{}, sshExtraArgs...)
	if util.IsSSHDebugEnabled() {
		args = append(args, "-v")
	}
	args = append(args, opts...)
	return append(args, m.hostArgs...)
}

// start opens the master connection and attaches the keeper session.
func (m *sshMaster) start(ctx context.Context, dir, target string) error {
	log.Debugf("Opening SSH connection to %s", target)

	// -f: authenticate in the foreground (prompts work), then background
	// itself in a new session, out of reach of Ctrl-C on our terminal.
	// The foreground process exits 0 once the connection is ready.
	master := osexec.CommandContext(ctx, sshBinary, m.baseArgs(
		"-f", "-N",
		"-o", controlPathOpt(dir),
		"-o", "ControlMaster=yes",
		"-o", "ControlPersist="+controlPersist,
	)...)
	// Files, not pipes: the backgrounded master inherits these, and pipes
	// would keep Wait from returning.
	master.Stdout = os.Stderr
	master.Stderr = os.Stderr
	if err := master.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("could not connect to %s (see the ssh error above): %w", target, err)
	}

	if err := m.startKeeper(dir); err != nil {
		m.exit(dir)
		return fmt.Errorf("connected to %s, but could not start the keepalive session: %w", target, err)
	}

	log.Debugf("SSH connection to %s ready (%s)", target, m.socket)
	return nil
}

// startKeeper runs `cat` on the remote host, reading from a pipe that only
// this process holds, and waits until it is running.
func (m *sshMaster) startKeeper(dir string) error {
	r, w, err := os.Pipe() // close-on-exec: no other child inherits w
	if err != nil {
		return err
	}

	args := append(append([]string{}, sshExtraArgs...), muxClientArgs(dir)...)
	args = append(args, m.hostArgs...)
	args = append(args, "echo ready && exec cat >/dev/null")

	cmd := osexec.Command(sshBinary, args...)
	cmd.Stdin = r
	// Own process group: Ctrl-C on the terminal must not end it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.Close()
		w.Close()
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return err
	}
	r.Close()

	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err == nil && strings.TrimSpace(line) != "ready" {
			err = fmt.Errorf("unexpected output %q", line)
		}
		ready <- err
	}()

	select {
	case err = <-ready:
	case <-time.After(keeperTimeout):
		err = errors.New("timed out")
	}
	if err != nil {
		w.Close()
		cmd.Process.Kill()
		cmd.Wait()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}

	m.keeperW = w
	m.keeper = cmd
	m.keeperDone = make(chan struct{})
	go func() {
		cmd.Wait()
		close(m.keeperDone)
	}()
	return nil
}

// alive reports whether the master is still running.
func (m *sshMaster) alive(dir string) bool {
	args := append(append([]string{}, sshExtraArgs...), "-o", controlPathOpt(dir), "-O", "check")
	args = append(args, m.hostArgs...)
	return osexec.Command(sshBinary, args...).Run() == nil
}

// exit stops the keeper and the master.
func (m *sshMaster) exit(dir string) {
	if m.keeperW != nil {
		m.keeperW.Close()
		select {
		case <-m.keeperDone:
		case <-time.After(5 * time.Second):
			m.keeper.Process.Kill()
			<-m.keeperDone
		}
	}

	args := append(append([]string{}, sshExtraArgs...), "-o", controlPathOpt(dir), "-O", "exit")
	args = append(args, m.hostArgs...)
	cmd := osexec.Command(sshBinary, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Debugf("Closing SSH master %s: %v: %s", m.socket, err, bytes.TrimSpace(out))
	}
}

// Close releases this reference; the last one shuts the master down.
func (c *sshConn) Close() error {
	c.closeOnce.Do(func() {
		muxRegistry.Lock()
		defer muxRegistry.Unlock()

		c.master.refs--
		if c.master.refs > 0 {
			return
		}
		c.master.exit(muxRegistry.dir)
		delete(muxRegistry.masters, c.master.socket)
		cleanupControlDirLocked()
	})
	return nil
}

// commandArgs returns the ssh arguments to run remote on this connection.
func (c *sshConn) commandArgs(remote string, tty bool) []string {
	muxRegistry.Lock()
	dir := muxRegistry.dir
	muxRegistry.Unlock()

	args := append([]string{}, sshExtraArgs...)
	args = append(args, muxClientArgs(dir)...)
	if util.IsSSHDebugEnabled() {
		args = append(args, "-v")
	} else {
		// Hides "Shared connection to ... closed." after tty sessions.
		args = append(args, "-o", "LogLevel=ERROR")
	}
	if tty {
		args = append(args, "-tt")
	}
	args = append(args, c.hostArgs...)
	return append(args, remote)
}

// lost reports whether the shared connection has gone away.
func (c *sshConn) lost() bool {
	muxRegistry.Lock()
	dir := muxRegistry.dir
	muxRegistry.Unlock()
	return dir == "" || !c.master.alive(dir)
}
