package exec

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		outUser string
		outHost string
		outPort string
	}{
		{
			name:    "without user or port",
			in:      "host",
			outUser: "",
			outHost: "host",
			outPort: "",
		},
		{
			name:    "with user but not port",
			in:      "user@host",
			outUser: "user",
			outHost: "host",
			outPort: "",
		},
		{
			name:    "without user but with port",
			in:      "host:222",
			outUser: "",
			outHost: "host",
			outPort: "222",
		},
		{
			name:    "with everything",
			in:      "user@host:222",
			outUser: "user",
			outHost: "host",
			outPort: "222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, host, port := parseTarget(tt.in)

			if user != tt.outUser {
				t.Errorf("unexpected user: \"%s\" != \"%s\"", user, tt.outUser)
			}
			if host != tt.outHost {
				t.Errorf("unexpected host: \"%s\" != \"%s\"", host, tt.outHost)
			}
			if port != tt.outPort {
				t.Errorf("unexpected port: \"%s\" != \"%s\"", port, tt.outPort)
			}
		})
	}
}

func TestIsParseableKnownHostsLine(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	validKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	fakeMLDSAKey := base64.StdEncoding.EncodeToString([]byte("not a real key blob"))

	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "blank line", line: "", want: true},
		{name: "comment", line: "# a comment", want: true},
		{name: "hostname only", line: "example.com", want: true},
		{name: "valid ed25519 entry", line: "example.com " + validKey, want: true},
		{name: "unsupported post-quantum key", line: "example.com ssh-mldsa44-ed25519 " + fakeMLDSAKey, want: false},
		{name: "unsupported key with cert marker", line: "@cert-authority example.com ssh-mldsa44-ed25519 " + fakeMLDSAKey, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isParseableKnownHostsLine(tt.line); got != tt.want {
				t.Errorf("isParseableKnownHostsLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestNewFilteredKnownHosts(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	validLine := "example.com " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	mldsaLine := "example.com ssh-mldsa44-ed25519 " + base64.StdEncoding.EncodeToString([]byte("not a real key blob"))

	dir := t.TempDir()
	file := filepath.Join(dir, "known_hosts")
	content := strings.Join([]string{"# comment", "", mldsaLine, validLine}, "\n") + "\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := knownhosts.New(file); err == nil {
		t.Fatal("expected knownhosts.New to fail on unsupported key algorithm")
	}

	kh, err := newFilteredKnownHosts([]string{file})
	if err != nil {
		t.Fatalf("newFilteredKnownHosts failed: %v", err)
	}

	algos := kh.HostKeyAlgorithms("example.com:22")
	if len(algos) != 1 || algos[0] != ssh.KeyAlgoED25519 {
		t.Errorf("HostKeyAlgorithms = %v, want [%s]", algos, ssh.KeyAlgoED25519)
	}
}

func TestBuildRemoteCmd(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		args []string
		want string
	}{
		{
			name: "safe args pass through unquoted",
			cmd:  "stat",
			args: []string{"-c", "%Y_%A_%n", "/nix/var/nix/profiles"},
			want: "stat -c %Y_%A_%n /nix/var/nix/profiles",
		},
		{
			name: "glob stays unquoted for remote expansion",
			cmd:  "stat",
			args: []string{"-c", "%n", "/nix/var/nix/profiles/*"},
			want: "stat -c %n /nix/var/nix/profiles/*",
		},
		{
			name: "args with spaces are single-quoted",
			cmd:  "sudo",
			args: []string{"/bin/sh", "-c", `nix-env -p "/nix/var/nix/profiles/system" --set "/nix/store/abc-x"`},
			want: `sudo /bin/sh -c 'nix-env -p "/nix/var/nix/profiles/system" --set "/nix/store/abc-x"'`,
		},
		{
			name: "embedded single quotes are escaped",
			cmd:  "sh",
			args: []string{"-c", "echo 'a b' c"},
			want: `sh -c 'echo '\''a b'\'' c'`,
		},
		{
			name: "no args",
			cmd:  "sudo",
			args: nil,
			want: "sudo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildRemoteCmd(tt.cmd, tt.args); got != tt.want {
				t.Errorf("buildRemoteCmd(%q, %v) = %q, want %q", tt.cmd, tt.args, got, tt.want)
			}
		})
	}
}
