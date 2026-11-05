package exec

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// These tests drive the real OpenSSH client against an in-process SSH
// server that runs commands locally with sh. They are skipped when ssh is
// not installed (e.g. in the Nix build sandbox).

type testSSHServer struct {
	ln      net.Listener
	hostKey ssh.Signer
	port    string
	logins  atomic.Int32

	mu    sync.Mutex
	conns []net.Conn
}

func newTestSSHServer(t *testing.T, authorized ssh.PublicKey) *testSSHServer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	conf := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authorized.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
	conf.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	s := &testSSHServer{ln: ln, hostKey: hostKey, port: port}
	t.Cleanup(s.stop)

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, c)
			s.mu.Unlock()
			go s.handle(c, conf)
		}
	}()
	return s
}

// stop closes the listener and drops every connection.
func (s *testSSHServer) stop() {
	s.ln.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
}

func (s *testSSHServer) handle(nc net.Conn, conf *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(nc, conf)
	if err != nil {
		nc.Close()
		return
	}
	defer sconn.Close()
	s.logins.Add(1)

	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		go handleExecSession(nch)
	}
}

func handleExecSession(nch ssh.NewChannel) {
	ch, reqs, err := nch.Accept()
	if err != nil {
		return
	}
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "exec":
		case "pty-req", "env":
			req.Reply(true, nil)
			continue
		default:
			req.Reply(false, nil)
			continue
		}

		var payload struct{ Command string }
		ssh.Unmarshal(req.Payload, &payload)
		req.Reply(true, nil)

		cmd := osexec.Command("sh", "-c", payload.Command)
		cmd.Stdout = ch
		cmd.Stderr = ch.Stderr()
		stdin, _ := cmd.StdinPipe()
		if err := cmd.Start(); err != nil {
			ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{127}))
			return
		}
		go func() {
			io.Copy(stdin, ch)
			stdin.Close()
		}()
		// Kill the command if the client goes away.
		done := make(chan struct{})
		go func() {
			for range reqs {
			}
			select {
			case <-done:
			default:
				cmd.Process.Kill()
			}
		}()

		status := 0
		if err := cmd.Wait(); err != nil {
			var eerr *osexec.ExitError
			if errors.As(err, &eerr) {
				status = eerr.ExitCode()
			}
		}
		close(done)
		ch.CloseWrite()
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
		return
	}
}

type sshTestSetup struct {
	srv        *testSSHServer
	dir        string
	knownHosts string
}

// setupSSHTest starts a server and points ssh at a private config with the
// host alias "testhost".
func setupSSHTest(t *testing.T) *sshTestSetup {
	t.Helper()
	if _, err := osexec.LookPath("ssh"); err != nil {
		t.Skip("ssh not installed")
	}

	dir := t.TempDir()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "test")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "id_test")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, _ := ssh.NewPublicKey(priv.Public())

	srv := newTestSSHServer(t, pub)

	knownHosts := filepath.Join(dir, "known_hosts")
	line := fmt.Sprintf("[127.0.0.1]:%s %s", srv.port, ssh.MarshalAuthorizedKey(srv.hostKey.PublicKey()))
	if err := os.WriteFile(knownHosts, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	config := filepath.Join(dir, "ssh_config")
	cfg := fmt.Sprintf(`Host testhost
  HostName 127.0.0.1
  Port %s
  User tester
Host *
  IdentityFile %s
  IdentitiesOnly yes
  IdentityAgent none
  UserKnownHostsFile %s
  GlobalKnownHostsFile /dev/null
  StrictHostKeyChecking yes
`, srv.port, keyPath, knownHosts)
	if err := os.WriteFile(config, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	oldArgs, oldPersist := sshExtraArgs, controlPersist
	sshExtraArgs = []string{"-F", config}
	t.Cleanup(func() {
		sshExtraArgs, controlPersist = oldArgs, oldPersist
		muxRegistry.Lock()
		leaked := len(muxRegistry.masters)
		muxRegistry.Unlock()
		if leaked != 0 {
			t.Errorf("%d SSH master(s) left open", leaked)
		}
	})

	return &sshTestSetup{srv: srv, dir: dir, knownHosts: knownHosts}
}

func newTestExecutor(t *testing.T, target string) *sshExecutor {
	t.Helper()
	e, err := NewSSHExecutor(context.Background(), target)
	if err != nil {
		t.Fatalf("connecting to %s: %v", target, err)
	}
	t.Cleanup(func() { e.(*sshExecutor).Close() })
	return e.(*sshExecutor)
}

func run(t *testing.T, e Executor, stdin string, cmd string, args ...string) (string, string, error) {
	t.Helper()
	c, err := e.Command(cmd, args...)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	c.SetStdout(&stdout)
	c.SetStderr(&stderr)
	if stdin != "" {
		c.SetStdin(strings.NewReader(stdin))
	}
	err = c.Run()
	return stdout.String(), stderr.String(), err
}

func TestSSHCommandsShareOneLogin(t *testing.T) {
	s := setupSSHTest(t)
	e := newTestExecutor(t, "testhost")

	out, _, err := run(t, e, "", "echo", "hello world")
	if err != nil || out != "hello world\n" {
		t.Fatalf("echo: %q, %v", out, err)
	}

	out, errOut, err := run(t, e, "", "sh", "-c", "echo out; echo err >&2; exit 3")
	if out != "out\n" || errOut != "err\n" {
		t.Errorf("stdout %q, stderr %q", out, errOut)
	}
	if ExitCode(err) != 3 {
		t.Errorf("exit code = %d (%v), want 3", ExitCode(err), err)
	}

	out, _, err = run(t, e, "piped input", "cat")
	if err != nil || out != "piped input" {
		t.Errorf("cat: %q, %v", out, err)
	}

	existing := filepath.Join(s.dir, "known_hosts")
	if ok, err := e.PathExists(existing); err != nil || !ok {
		t.Errorf("PathExists(existing) = %v, %v", ok, err)
	}
	if ok, err := e.PathExists(filepath.Join(s.dir, "missing")); err != nil || ok {
		t.Errorf("PathExists(missing) = %v, %v", ok, err)
	}

	// An ssh started the way nix starts it reuses the connection too.
	env := NixSSHOptsEnv()
	opts := strings.Fields(strings.TrimPrefix(env, "NIX_SSHOPTS="))
	nixLike := osexec.Command("ssh", append(opts, "testhost", "echo", "from-nix")...)
	if got, err := nixLike.Output(); err != nil || string(got) != "from-nix\n" {
		t.Errorf("ssh with NIX_SSHOPTS: %q, %v", got, err)
	}

	if n := s.srv.logins.Load(); n != 1 {
		t.Errorf("server saw %d logins, want 1", n)
	}
}

func TestSSHHostFileOperations(t *testing.T) {
	s := setupSSHTest(t)

	dir := filepath.Join(s.dir, "profiles")
	os.Mkdir(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "file with space"), []byte("content"), 0o644)
	os.Symlink("/nix/store/abc-system", filepath.Join(dir, "system-1-link"))

	h, err := NewHost(context.Background(), "testhost")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	entries, err := h.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = e.IsSymlink
	}
	if len(names) != 2 || !names["system-1-link"] {
		t.Errorf("ReadDir = %+v", entries)
	}

	if target, err := h.Readlink(filepath.Join(dir, "system-1-link")); err != nil || target != "/nix/store/abc-system" {
		t.Errorf("Readlink = %q, %v", target, err)
	}
	if data, err := h.ReadFile(filepath.Join(dir, "file with space")); err != nil || string(data) != "content" {
		t.Errorf("ReadFile = %q, %v", data, err)
	}
	if err := h.Remove(filepath.Join(dir, "system-1-link")); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if _, err := h.Lstat(filepath.Join(dir, "system-1-link")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Lstat after Remove: %v", err)
	}
}

func TestSSHTargetsResolvingToSameHostShareMaster(t *testing.T) {
	s := setupSSHTest(t)

	a := newTestExecutor(t, "testhost")
	b := newTestExecutor(t, "tester@testhost")
	if a.conn.master != b.conn.master {
		t.Fatal("expected one shared master")
	}

	a.Close()
	if out, _, err := run(t, b, "", "echo", "still"); err != nil || out != "still\n" {
		t.Fatalf("after closing one reference: %q, %v", out, err)
	}

	dir := muxRegistry.dir
	b.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("control directory %s not removed: %v", dir, err)
	}
	if env := NixSSHOptsEnv(); env != "" {
		t.Errorf("NixSSHOptsEnv after close = %q", env)
	}
	if n := s.srv.logins.Load(); n != 1 {
		t.Errorf("server saw %d logins, want 1", n)
	}
}

func TestSSHKeeperHoldsMasterOpen(t *testing.T) {
	setupSSHTest(t)
	controlPersist = "1"

	e := newTestExecutor(t, "testhost")
	time.Sleep(2500 * time.Millisecond)

	if out, _, err := run(t, e, "", "echo", "alive"); err != nil || out != "alive\n" {
		t.Fatalf("after idling past ControlPersist: %q, %v", out, err)
	}
}

// If nilla dies, the keeper's stdin pipe closes and the master exits on its
// own after ControlPersist.
func TestSSHMasterExitsWhenKeeperPipeCloses(t *testing.T) {
	setupSSHTest(t)
	controlPersist = "1"

	e := newTestExecutor(t, "testhost")
	e.conn.master.keeperW.Close() // what the kernel does when nilla exits

	deadline := time.Now().Add(10 * time.Second)
	for e.conn.master.alive(muxRegistry.dir) {
		if time.Now().After(deadline) {
			t.Fatal("master still running after the keeper pipe closed")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestSSHConnectFailure(t *testing.T) {
	s := setupSSHTest(t)
	os.WriteFile(s.knownHosts, nil, 0o600) // host key now unknown

	_, err := NewSSHExecutor(context.Background(), "testhost")
	if err == nil || !strings.Contains(err.Error(), "could not connect to testhost") {
		t.Fatalf("unexpected error: %v", err)
	}
	if muxRegistry.dir != "" {
		t.Errorf("control directory %s left behind", muxRegistry.dir)
	}
}

func TestSSHLostConnection(t *testing.T) {
	s := setupSSHTest(t)
	e := newTestExecutor(t, "testhost")

	s.srv.stop()
	deadline := time.Now().Add(10 * time.Second)
	for e.conn.master.alive(muxRegistry.dir) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	_, _, err := run(t, e, "", "echo", "hi")
	if err == nil || !strings.Contains(err.Error(), "lost SSH connection to testhost") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSSHCommandCancel(t *testing.T) {
	setupSSHTest(t)
	e := newTestExecutor(t, "testhost")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	c, err := e.CommandContext(ctx, "sleep", "30")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := c.Run(); err == nil {
		t.Fatal("expected an error from the cancelled command")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cancel took %s", elapsed)
	}

	// The connection is still usable afterwards.
	if out, _, err := run(t, e, "", "echo", "ok"); err != nil || out != "ok\n" {
		t.Fatalf("after cancel: %q, %v", out, err)
	}
}

func TestNixSSHOptsEnv(t *testing.T) {
	setupSSHTest(t)
	if env := NixSSHOptsEnv(); env != "" {
		t.Fatalf("without connections: %q", env)
	}

	t.Setenv("NIX_SSHOPTS", "-o Compression=yes")
	newTestExecutor(t, "testhost")

	env := NixSSHOptsEnv()
	for _, want := range []string{
		"ControlPath=" + filepath.Join(muxRegistry.dir, "%C"),
		"ControlMaster=no",
		"BatchMode=yes",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("%q does not contain %q", env, want)
		}
	}
	if !strings.HasSuffix(env, " -o Compression=yes") {
		t.Errorf("existing NIX_SSHOPTS not kept last: %q", env)
	}
}
