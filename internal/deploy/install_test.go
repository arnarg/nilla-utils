package deploy

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-test/deep"
)

func TestParseDiskMode(t *testing.T) {
	for _, s := range []string{"none", "mount", "format", "destroy"} {
		m, err := ParseDiskMode(s)
		if err != nil || string(m) != s {
			t.Errorf("ParseDiskMode(%q) = %q, %v", s, m, err)
		}
	}
	if m, err := ParseDiskMode(""); err != nil || m != "" {
		t.Errorf("empty mode should be unset, got %q, %v", m, err)
	}
	if _, err := ParseDiskMode("wipe"); err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestDiskScriptAttrs(t *testing.T) {
	tests := map[DiskMode][]string{
		DiskNone:    nil,
		DiskMount:   {"mountScript"},
		DiskFormat:  {"formatScript", "mountScript"},
		DiskDestroy: {"diskoScript"},
	}
	for mode, want := range tests {
		if got := diskScriptAttrs(mode); !slices.Equal(got, want) {
			t.Errorf("diskScriptAttrs(%s) = %v, want %v", mode, got, want)
		}
	}
}

func TestParseDiskoInfo(t *testing.T) {
	t.Run("null means no disko", func(t *testing.T) {
		info, err := parseDiskoInfo([]byte("null"))
		if err != nil || info != nil {
			t.Errorf("got %v, %v", info, err)
		}
	})
	t.Run("no disks means no disko", func(t *testing.T) {
		info, err := parseDiskoInfo([]byte(`{"rootMountPoint":"/mnt","disks":{}}`))
		if err != nil || info != nil {
			t.Errorf("got %v, %v", info, err)
		}
	})
	t.Run("disks", func(t *testing.T) {
		info, err := parseDiskoInfo([]byte(`{"rootMountPoint":"/mnt","disks":{"main":"/dev/vda"},"swaps":["/dev/vda2"]}`))
		if err != nil {
			t.Fatal(err)
		}
		want := &diskoInfo{RootMountPoint: "/mnt", Disks: map[string]string{"main": "/dev/vda"}, Swaps: []string{"/dev/vda2"}}
		if diff := deep.Equal(info, want); diff != nil {
			t.Error(diff)
		}
	})
}

func TestResolveDiskMode(t *testing.T) {
	disko := &diskoInfo{Disks: map[string]string{"main": "/dev/vda"}}

	tests := []struct {
		name      string
		requested DiskMode
		info      *diskoInfo
		want      DiskMode
		wantErr   bool
	}{
		{name: "no disko defaults to none", info: nil, want: DiskNone},
		{name: "no disko accepts none", requested: DiskNone, info: nil, want: DiskNone},
		{name: "no disko rejects destroy", requested: DiskDestroy, info: nil, wantErr: true},
		{name: "no disko rejects mount", requested: DiskMount, info: nil, wantErr: true},
		{name: "disko requires explicit mode", info: disko, wantErr: true},
		{name: "disko destroy", requested: DiskDestroy, info: disko, want: DiskDestroy},
		{name: "disko none", requested: DiskNone, info: disko, want: DiskNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDiskMode(tt.requested, tt.info)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveRoot(t *testing.T) {
	disko := &diskoInfo{RootMountPoint: "/target", Disks: map[string]string{"main": "/dev/vda"}}

	tests := []struct {
		name    string
		opts    InstallOptions
		mode    DiskMode
		info    *diskoInfo
		want    string
		wantErr bool
	}{
		{name: "default", want: "/mnt", mode: DiskNone},
		{name: "flag without disko", opts: InstallOptions{Root: "/a", RootSet: true}, mode: DiskNone, want: "/a"},
		{name: "disko mount point used", opts: InstallOptions{Root: "/mnt"}, mode: DiskMount, info: disko, want: "/target"},
		{name: "matching flag", opts: InstallOptions{Root: "/target", RootSet: true}, mode: DiskDestroy, info: disko, want: "/target"},
		{name: "conflicting flag", opts: InstallOptions{Root: "/mnt", RootSet: true}, mode: DiskDestroy, info: disko, wantErr: true},
		{name: "disko with none uses flag", opts: InstallOptions{Root: "/mnt"}, mode: DiskNone, info: disko, want: "/mnt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveRoot(tt.opts, tt.mode, tt.info)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseFacts(t *testing.T) {
	got := parseFacts("uid=0\nvariant=installer\nnixos_install=1\nroot_mounted=1\n")
	want := targetFacts{UID: "0", VariantID: "installer", HasNixOSInstall: true, RootMounted: true, RootMountedKnown: true}
	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}

	got = parseFacts("uid=1000\nroot_mounted=0\n")
	want = targetFacts{UID: "1000", RootMountedKnown: true}
	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}

func TestValidateFacts(t *testing.T) {
	good := targetFacts{UID: "0", VariantID: "installer", HasNixOSInstall: true, RootMounted: true, RootMountedKnown: true}

	tests := []struct {
		name    string
		facts   func(f targetFacts) targetFacts
		mode    DiskMode
		opts    InstallOptions
		wantErr string
	}{
		{name: "ok", mode: DiskNone},
		{name: "not root", facts: func(f targetFacts) targetFacts { f.UID = "1000"; return f }, mode: DiskNone, wantErr: "as root"},
		{name: "not installer", facts: func(f targetFacts) targetFacts { f.VariantID = ""; return f }, mode: DiskNone, wantErr: "installer"},
		{name: "not installer but skipped", facts: func(f targetFacts) targetFacts { f.VariantID = ""; return f }, mode: DiskNone, opts: InstallOptions{NoInstallerCheck: true}},
		{name: "no nixos-install", facts: func(f targetFacts) targetFacts { f.HasNixOSInstall = false; return f }, mode: DiskNone, wantErr: "nixos-install"},
		{name: "root not mounted with none", facts: func(f targetFacts) targetFacts { f.RootMounted = false; return f }, mode: DiskNone, wantErr: "not a mount point"},
		{name: "root not mounted with destroy", facts: func(f targetFacts) targetFacts { f.RootMounted = false; return f }, mode: DiskDestroy},
		{name: "root mounted with destroy", mode: DiskDestroy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := good
			if tt.facts != nil {
				f = tt.facts(f)
			}
			err := validateFacts(f, tt.mode, "/mnt", tt.opts)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseDiskCheck(t *testing.T) {
	disks := map[string]string{
		"main": "/dev/disk/by-id/nvme-a",
		"data": "/dev/sdb",
	}
	out := "1\t/dev/disk/by-id/nvme-a\t 476.9G Samsung  SSD 980\n0\t/dev/sdb\t\n"

	got := parseDiskCheck(out, disks)
	want := []diskState{
		{Name: "data", Device: "/dev/sdb", Present: false},
		{Name: "main", Device: "/dev/disk/by-id/nvme-a", Present: true, Info: "476.9G Samsung SSD 980"},
	}
	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}

	// Devices missing from the output are treated as not present.
	got = parseDiskCheck("", map[string]string{"main": "/dev/vda"})
	if len(got) != 1 || got[0].Present {
		t.Errorf("got %+v, want a single missing disk", got)
	}
}

func TestInstallStoreURL(t *testing.T) {
	tests := []struct {
		name                   string
		user, host, port, root string
		want                   string
	}{
		{name: "live store", user: "root", host: "h", want: "ssh://root@h"},
		{name: "no user", host: "h", want: "ssh://h"},
		{name: "port", user: "root", host: "h", port: "2222", want: "ssh://root@h?port=2222"},
		{name: "root store", user: "root", host: "h", root: "/mnt", want: "ssh://root@h?remote-store=local%3Froot%3D%2Fmnt"},
		{name: "root store with port", user: "root", host: "h", port: "22", root: "/mnt", want: "ssh://root@h?port=22&remote-store=local%3Froot%3D%2Fmnt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := installStoreURL(tt.user, tt.host, tt.port, tt.root); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNixosInstallArgs(t *testing.T) {
	got := nixosInstallArgs("/mnt", "/nix/store/abc-nixos-system")
	want := []string{"nixos-install", "--root", "/mnt", "--no-root-passwd", "--no-channel-copy", "--store-path", "/nix/store/abc-nixos-system"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWriteExtraFilesArchive(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "etc", "ssh"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "etc", "ssh", "ssh_host_ed25519_key"), []byte("secret"), 0o600))
	must(os.Symlink("ssh_host_ed25519_key", filepath.Join(dir, "etc", "ssh", "link")))

	var buf bytes.Buffer
	must(writeExtraFilesArchive(&buf, dir))

	type entry struct {
		Name     string
		Type     byte
		Mode     int64
		Uid, Gid int
		Link     string
		Content  string
	}
	var got []entry
	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		must(err)
		content, err := io.ReadAll(tr)
		must(err)
		got = append(got, entry{hdr.Name, hdr.Typeflag, hdr.Mode & 0o777, hdr.Uid, hdr.Gid, hdr.Linkname, string(content)})
	}

	want := []entry{
		{Name: "etc/", Type: tar.TypeDir, Mode: 0o755},
		{Name: "etc/ssh/", Type: tar.TypeDir, Mode: 0o755},
		{Name: "etc/ssh/link", Type: tar.TypeSymlink, Mode: 0o777, Link: "ssh_host_ed25519_key"},
		{Name: "etc/ssh/ssh_host_ed25519_key", Type: tar.TypeReg, Mode: 0o600, Content: "secret"},
	}
	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}
