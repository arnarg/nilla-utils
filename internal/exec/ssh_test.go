package exec

import "testing"

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
