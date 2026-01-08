package exec

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/log"
	"github.com/kevinburke/ssh_config"
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

const (
	sshAuthSock       = "SSH_AUTH_SOCK"
	defaultKnownHosts = "~/.ssh/known_hosts"
)

type sshExecutor struct {
	client *ssh.Client
}

func NewSSHExecutor(target string) (Executor, error) {
	// Get config from target
	host, conf, err := configFromTarget(target)
	if err != nil {
		return nil, err
	}

	if util.IsSSHDebugEnabled() {
		log.Debugf("Connecting to %s@%s", conf.User, host)
	}

	// Connect to host
	client, err := ssh.Dial("tcp", host, conf)
	if err != nil {
		// Enhance error message with more context
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, fmt.Errorf("SSH authentication failed for %s@%s: %v\n\nPossible causes:\n  - SSH keys are not in the server's authorized_keys file for user %s\n  - Keys are encrypted and require a passphrase (not supported)\n  - Server only accepts specific key types\n\nRun with -v to see which keys were attempted", conf.User, host, err, conf.User)
		}
		return nil, err
	}

	if util.IsSSHDebugEnabled() {
		log.Debugf("Successfully connected to %s@%s", conf.User, host)
	}

	return &sshExecutor{client}, nil
}

func (e *sshExecutor) Command(cmd string, args ...string) (Command, error) {
	return e.CommandContext(context.Background(), cmd, args...)
}

func (e *sshExecutor) CommandContext(ctx context.Context, cmd string, args ...string) (Command, error) {
	// Try to start a new session
	sess, err := e.client.NewSession()
	if err != nil {
		return nil, err
	}

	return &sshCommand{sess, cmd, args, -1, nil, ctx}, nil
}

func (e *sshExecutor) PathExists(path string) (bool, error) {
	cmd, err := e.Command("ls", path)
	if err != nil {
		return false, err
	}

	if err := cmd.Run(); err != nil {
		if eerr, ok := err.(*ssh.ExitError); ok {
			return eerr.ExitStatus() != 0, nil
		}
		return false, err
	}

	return true, nil
}

func (e *sshExecutor) IsLocal() bool {
	return false
}

type sshCommand struct {
	sess *ssh.Session
	cmd  string
	args []string

	fd    int
	state *term.State
	ctx   context.Context
}

func (c *sshCommand) SetStdin(r io.Reader) {
	c.sess.Stdin = r
}

func (c *sshCommand) SetStdout(w io.Writer) {
	c.sess.Stdout = w
}

func (c *sshCommand) SetStderr(w io.Writer) {
	c.sess.Stderr = w
}

func (c *sshCommand) StdinPipe() (io.WriteCloser, error) {
	return c.sess.StdinPipe()
}

func (c *sshCommand) StdoutPipe() (io.Reader, error) {
	return c.sess.StdoutPipe()
}

func (c *sshCommand) StderrPipe() (io.Reader, error) {
	return c.sess.StderrPipe()
}

func (c *sshCommand) Run() error {
	defer c.cleanup()
	if err := c.Start(); err != nil {
		return err
	}

	return c.Wait()
}

func (c *sshCommand) Start() error {
	// Build command string
	cmd := fmt.Sprintf("%s %s", c.cmd, strings.Join(c.args, " "))

	// If we're running sudo, we should request a pty
	if c.cmd == "sudo" && c.sess.Stdin != nil {
		// Set up terminal modes
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,     // disable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		}

		// Request pseudo terminal
		if f, ok := c.sess.Stdin.(*os.File); ok {
			fileDescriptor := int(f.Fd())
			if term.IsTerminal(fileDescriptor) {
				state, err := term.MakeRaw(fileDescriptor)
				if err != nil {
					return err
				}
				c.state = state
				c.fd = fileDescriptor

				termWidth, termHeight, err := term.GetSize(fileDescriptor)
				if err != nil {
					return err
				}

				err = c.sess.RequestPty("xterm-256color", termHeight, termWidth, modes)
				if err != nil {
					return err
				}
			} else {
				// Try to request PTY anyway with default size (might work in some cases)
				err := c.sess.RequestPty("xterm-256color", 80, 24, modes)
				if err != nil {
					log.Debugf("Failed to allocate PTY for sudo command: %v", err)
				}
			}
		}
	}

	// Start command
	return c.sess.Start(cmd)
}

func (c *sshCommand) Wait() error {
	defer c.sess.Close()
	defer c.cleanup()

	var wg sync.WaitGroup

	lctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		defer wg.Done()

		select {
		// Global context cancelled
		case <-c.ctx.Done():
			// Send sigint to session
			if err := c.sess.Signal(ssh.SIGINT); err != nil {
				fmt.Println(err)
			}

		// Local context cancelled
		case <-lctx.Done():
		}
	}()

	// Wait for session command
	err := c.sess.Wait()

	// Cancel local context
	cancel()

	// Wait for goroutine
	wg.Wait()

	return err
}

func (c *sshCommand) cleanup() {
	if c.state != nil {
		term.Restore(c.fd, c.state)
		c.state = nil
	}
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

	// Check if port is specified
	parts = strings.SplitN(host, ":", 2)

	// If there's 2 parts, we have host:port
	if len(parts) == 2 {
		host = parts[0]
		port = parts[1]
	}

	return
}

func configFromTarget(target string) (string, *ssh.ClientConfig, error) {
	// Get user and host
	user, host, port := parseTarget(target)

	// Set port, if not specified
	if port == "" {
		port = ssh_config.Get(host, "Port")
		// Still not set
		if port == "" {
			port = ssh_config.Default("Port")
		}
	}

	// Build config from host
	config, err := buildDefaultConfig(host, port)
	if err != nil {
		return "", nil, err
	}

	// Override if specified
	if user != "" {
		config.User = user
	}

	// If user is still unset, we use current user
	if config.User == "" {
		config.User = util.GetUser()
	}

	// Add password callback
	isTerminal := term.IsTerminal(int(os.Stdin.Fd()))
	if isTerminal {
		config.Auth = append(
			config.Auth,
			ssh.PasswordCallback(func() (string, error) {
				fmt.Printf("%s@%s's password:\n", config.User, host)
				password, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return "", err
				}
				return string(password), nil
			}),
		)
	}

	// Validate that we have at least one authentication method
	// Pre-check if public keys are available when password auth is not available
	if !isTerminal && len(config.Auth) == 1 {
		// Only have public key auth and no terminal, check if keys are actually available
		settings := ssh_config.DefaultUserSettings
		identitiesOnly := false
		if ionly := settings.Get(host, "IdentitiesOnly"); ionly == "yes" {
			identitiesOnly = true
		}
		identityFiles := getIdentityFiles(settings, host)
		agentPath := getAgentPath(settings, host)

		// Try to load keys to see if any are available
		keys := []ssh.Signer{}
		if !identitiesOnly {
			agentKeys, err := loadAgentKeys(agentPath)
			if err == nil && agentKeys != nil {
				keys = append(keys, agentKeys...)
			}
		}
		for _, path := range identityFiles {
			key, err := loadPrivateKeyFromFS(path)
			if err == nil && key != nil {
				keys = append(keys, key)
			}
		}

		if len(keys) == 0 {
			return "", nil, fmt.Errorf("no SSH keys found for %s@%s (checked SSH agent and identity files) and password authentication is not available in non-terminal context. Please ensure SSH keys are available or run in a terminal", config.User, host)
		}
	}

	return fmt.Sprintf("%s:%s", host, port), config, nil
}

func buildDefaultConfig(host, port string) (*ssh.ClientConfig, error) {
	// SSH config file parser
	settings := ssh_config.DefaultUserSettings

	// Initial config
	conf := &ssh.ClientConfig{
		User: settings.Get(host, "User"),
		Auth: []ssh.AuthMethod{},
	}

	// Check IdentitiesOnly
	identitiesOnly := false
	if ionly := settings.Get(host, "IdentitiesOnly"); ionly == "yes" {
		identitiesOnly = true
	}

	// Get IdentityFiles
	identityFiles := getIdentityFiles(settings, host)

	// Get agent path
	agentPath := getAgentPath(settings, host)

	// Make pubkey callback
	conf.Auth = append(
		conf.Auth,
		ssh.PublicKeysCallback(
			newPublicKeysCallback(identitiesOnly, agentPath, identityFiles),
		),
	)

	// Get known hosts file
	knownHostsFiles := getKnownHostsFiles(settings, host)
	kh, err := knownhosts.New(knownHostsFiles...)
	if err == nil {
		conf.HostKeyCallback = kh.HostKeyCallback()
		conf.HostKeyAlgorithms = kh.HostKeyAlgorithms(fmt.Sprintf("%s:%s", host, port))
	} else {
		return nil, err
	}

	return conf, nil
}

func getKnownHostsFiles(settings *ssh_config.UserSettings, host string) []string {

	if f, err := settings.GetStrict(host, "UserKnownHostsFile"); err == nil {
		files := []string{}

		for _, khf := range strings.Split(f, " ") {
			resolved := resolvePath(khf)

			if _, err := os.Stat(resolved); err == nil {
				files = append(files, resolved)
			}
		}

		return files
	}

	return []string{resolvePath(defaultKnownHosts)}
}

func resolvePath(p string) string {
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(util.GetHomeDir(), p[2:])
	}

	return os.ExpandEnv(p)
}

func configPath(p string) string {
	return fmt.Sprintf("%s/.ssh/%s", util.GetHomeDir(), p)
}

func getIdentityFiles(settings *ssh_config.UserSettings, host string) []string {
	files := []string{}

	configFiles := settings.GetAll(host, "IdentityFile")
	for _, f := range configFiles {
		resolved := resolvePath(f)
		files = append(files, resolved)
	}

	// Default paths to check
	defaultFiles := []string{
		configPath("id_dsa"),
		configPath("id_ecdsa"),
		configPath("id_ecdsa_sk"),
		configPath("id_ed25519"),
		configPath("id_ed25519_sk"),
		configPath("id_xmsshost"),
		configPath("id_rsa"),
	}
	files = append(files, defaultFiles...)

	return files
}

func getAgentPath(settings *ssh_config.UserSettings, host string) string {
	identityAgent := settings.Get(host, "IdentityAgent")
	if identityAgent != "" && identityAgent != sshAuthSock {
		return resolvePath(identityAgent)
	}
	if identityAgent == "none" {
		return ""
	}

	return os.Getenv(sshAuthSock)
}

func loadAgentKeys(agentPath string) ([]ssh.Signer, error) {
	if agentPath == "" {
		return nil, nil
	}

	conn, err := net.Dial("unix", agentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		return nil, err
	}
	o := []ssh.Signer{}
	for _, s := range signers {
		// Test if key can sign (some agents require user interaction)
		_, err := s.Sign(rand.Reader, []byte(""))
		// Include the key anyway - the sign test might be too strict for some agents
		// The SSH library will handle authentication failures gracefully
		o = append(o, s)
		if err != nil && util.IsSSHDebugEnabled() {
			log.Debugf("SSH agent: key type %s failed sign test but will try during auth", s.PublicKey().Type())
		}
	}
	return o, nil
}

func loadPrivateKeyFromFS(path string) (ssh.Signer, error) {
	privateKey, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		// Check if it's an encrypted key
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return nil, fmt.Errorf("encrypted key %s requires passphrase (not supported)", path)
		}
		return nil, err
	}
	return signer, nil
}

func newPublicKeysCallback(identitiesOnly bool, agentPath string, identityFiles []string) func() ([]ssh.Signer, error) {
	return func() ([]ssh.Signer, error) {
		keys := []ssh.Signer{}
		keyTypes := []string{}
		if !identitiesOnly {
			agentKeys, err := loadAgentKeys(agentPath)
			if err == nil && agentKeys != nil {
				keys = append(keys, agentKeys...)
				for _, k := range agentKeys {
					keyTypes = append(keyTypes, k.PublicKey().Type())
				}
			}
		}
		for _, path := range identityFiles {
			key, err := loadPrivateKeyFromFS(path)
			if err == nil && key != nil {
				keys = append(keys, key)
				keyTypes = append(keyTypes, key.PublicKey().Type())
			}
		}
		if len(keys) > 0 && util.IsSSHDebugEnabled() {
			log.Debugf("SSH authentication: %d key(s) available (types: %s)", len(keys), strings.Join(keyTypes, ", "))
		}
		return keys, nil
	}
}
