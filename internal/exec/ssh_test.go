package exec

import "testing"

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
