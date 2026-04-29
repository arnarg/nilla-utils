package askpass

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/arnarg/nilla-utils/internal/util"
	"golang.org/x/term"
)

type Server struct {
	listener   net.Listener
	socketPath string
	dirPath    string

	mu      sync.Mutex
	cache   *PasswordCache
	pending map[string]*sync.Cond
}

func NewServer(cache *PasswordCache) (*Server, func(), error) {
	socketPath, dirPath, err := createSocketPath()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create askpass socket path: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		os.Remove(socketPath)
		os.Remove(dirPath)
		return nil, nil, fmt.Errorf("failed to listen on askpass socket: %w", err)
	}

	os.Chmod(socketPath, 0600)

	srv := &Server{
		listener:   listener,
		socketPath: socketPath,
		dirPath:    dirPath,
		cache:      cache,
		pending:    make(map[string]*sync.Cond),
	}

	cleanup := func() {
		listener.Close()
		os.Remove(socketPath)
		os.Remove(dirPath)
	}

	return srv, cleanup, nil
}

func createSocketPath() (socketPath, dirPath string, err error) {
	if rdir := os.Getenv("XDG_RUNTIME_DIR"); rdir != "" {
		dirPath = filepath.Join(rdir, "nilla-utils")
	} else {
		dirPath = filepath.Join(os.TempDir(), fmt.Sprintf("nilla-utils-%d", os.Getuid()))
	}

	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return "", "", err
	}

	socketPath = filepath.Join(dirPath, fmt.Sprintf("askpass-%d.sock", os.Getpid()))
	return socketPath, dirPath, nil
}

func (s *Server) SocketPath() string {
	return s.socketPath
}

func (s *Server) Serve(ctx context.Context) error {
	var closed atomic.Bool
	go func() {
		<-ctx.Done()
		closed.Store(true)
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if closed.Load() {
				return nil
			}
			return err
		}
		go s.handleConn(conn)
	}
}

type request struct {
	host      string
	commandID string
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	req, err := readRequest(conn)
	if err != nil {
		return
	}

	s.mu.Lock()
	if password, ok := s.cache.Get(req.host); ok {
		s.mu.Unlock()
		fmt.Fprintln(conn, password)
		return
	}

	if cond, ok := s.pending[req.host]; ok {
		s.mu.Unlock()
		cond.L.Lock()
		cond.Wait()
		cond.L.Unlock()
		password, _ := s.cache.Get(req.host)
		s.mu.Unlock()
		fmt.Fprintln(conn, password)
		return
	}

	cond := sync.NewCond(&sync.Mutex{})
	s.pending[req.host] = cond
	s.mu.Unlock()

	password := s.promptUser(req.host)

	s.cache.Set(req.host, password)

	s.mu.Lock()
	delete(s.pending, req.host)
	s.mu.Unlock()

	cond.Broadcast()
	fmt.Fprintln(conn, password)
}

func (s *Server) promptUser(host string) string {
	fmt.Fprintf(os.Stderr, "%s's password: ", host)
	password, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(password)
}

func readRequest(conn net.Conn) (*request, error) {
	reader := bufio.NewReader(conn)
	host, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	commandID, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return &request{
		host:      strings.TrimRight(host, "\n"),
		commandID: strings.TrimRight(commandID, "\n"),
	}, nil
}

func ParseHostFromPrompt(prompt string) string {
	// Standard password auth: "user@host's password: "
	if idx := strings.Index(prompt, "'s"); idx > 0 {
		target := prompt[:idx]
		user, hostname := util.ParseTarget(target)
		if user == "" {
			user = util.GetUser()
		}
		return fmt.Sprintf("%s@%s", user, hostname)
	}

	// Keyboard-interactive auth: "(user@host) Password: "
	if strings.HasPrefix(prompt, "(") {
		if idx := strings.Index(prompt, ")"); idx > 1 {
			target := prompt[1:idx]
			user, hostname := util.ParseTarget(target)
			if user == "" {
				user = util.GetUser()
			}
			return fmt.Sprintf("%s@%s", user, hostname)
		}
	}

	return prompt
}

func GetPassword(socketPath, host, commandID string) (string, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return "", fmt.Errorf("failed to connect to askpass socket: %w", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "%s\n%s\n", host, commandID)

	reader := bufio.NewReader(conn)
	password, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read password from askpass server: %w", err)
	}

	return strings.TrimRight(password, "\n"), nil
}
