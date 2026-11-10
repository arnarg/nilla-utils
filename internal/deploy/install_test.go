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

func TestParseInstallInfo(t *testing.T) {
	t.Run("no disko", func(t *testing.T) {
		info, err := parseInstallInfo([]byte(`{"disko":null,"substituters":[],"trustedPublicKeys":[]}`))
		if err != nil || info.Disko != nil {
			t.Errorf("got %+v, %v", info, err)
		}
	})
	t.Run("no disks means no disko", func(t *testing.T) {
		info, err := parseInstallInfo([]byte(`{"disko":{"rootMountPoint":"/mnt","disks":{}}}`))
		if err != nil || info.Disko != nil {
			t.Errorf("got %+v, %v", info, err)
		}
	})
	t.Run("disks and caches", func(t *testing.T) {
		info, err := parseInstallInfo([]byte(`{
			"disko":{"rootMountPoint":"/mnt","disks":{"main":"/dev/vda"},"swaps":["/dev/vda2"]},
			"substituters":["https://cache.example"],
			"trustedPublicKeys":["cache.example:abc="]
		}`))
		if err != nil {
			t.Fatal(err)
		}
		want := &installInfo{
			Disko:             &diskoInfo{RootMountPoint: "/mnt", Disks: map[string]string{"main": "/dev/vda"}, Swaps: []string{"/dev/vda2"}},
			Substituters:      []string{"https://cache.example"},
			TrustedPublicKeys: []string{"cache.example:abc="},
		}
		if diff := deep.Equal(info, want); diff != nil {
			t.Error(diff)
		}
	})
}

func TestResolvePlacement(t *testing.T) {
	tests := []struct {
		plan *Plan
		want buildPlacement
	}{
		{&Plan{DeployTarget: "root@t"}, buildLocal},
		{&Plan{DeployTarget: "root@t", BuildTarget: "root@t"}, buildOnTarget},
		{&Plan{DeployTarget: "root@t", BuildTarget: "me@builder"}, buildOnBuilder},
	}
	for _, tt := range tests {
		if got := resolvePlacement(tt.plan); got != tt.want {
			t.Errorf("resolvePlacement(%+v) = %v, want %v", tt.plan, got, tt.want)
		}
	}
}

func TestRemoteStoreURL(t *testing.T) {
	tests := []struct {
		user, host, port, root string
		want                   string
	}{
		{"root", "h", "", "", "ssh-ng://root@h"},
		{"", "h", "2222", "", "ssh-ng://h?port=2222"},
		{"root", "h", "2222", "/mnt", "ssh-ng://root@h?port=2222&remote-store=local%3Froot%3D%2Fmnt"},
		{"root", "::1", "", "", "ssh-ng://root@[::1]"},
	}
	for _, tt := range tests {
		if got := remoteStoreURL(tt.user, tt.host, tt.port, tt.root); got != tt.want {
			t.Errorf("remoteStoreURL(%q, %q, %q, %q) = %q, want %q", tt.user, tt.host, tt.port, tt.root, got, tt.want)
		}
	}
}

func TestSubstituterArgs(t *testing.T) {
	if got := substituterArgs(&installInfo{}); got != nil {
		t.Errorf("expected no args, got %v", got)
	}
	got := substituterArgs(&installInfo{
		Substituters:      []string{"https://a", "https://b"},
		TrustedPublicKeys: []string{"a:1=", "b:2="},
	})
	want := []string{
		"--option", "extra-substituters", "https://a https://b",
		"--option", "extra-trusted-public-keys", "a:1= b:2=",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTargetBuildArgs(t *testing.T) {
	drvs := []string{"/nix/store/a.drv"}
	extra := []string{"--option", "extra-substituters", "https://a"}

	got := targetBuildArgs("/mnt", extra, drvs)
	want := []string{"--store", "/mnt", "--no-link", "--option", "extra-substituters", "https://a", "/nix/store/a.drv^out"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	got = targetBuildArgs("", nil, drvs)
	want = []string{"--no-link", "/nix/store/a.drv^out"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDrvPathsApply(t *testing.T) {
	got := drvPathsApply([]string{"toplevel", "diskoScript"})
	want := `b: map (n: b.${n}.drvPath) [ "toplevel" "diskoScript" ]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputInstallables(t *testing.T) {
	got := outputInstallables([]string{"/nix/store/a.drv", "/nix/store/b.drv"})
	want := []string{"/nix/store/a.drv^out", "/nix/store/b.drv^out"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPick(t *testing.T) {
	drvs := map[string]string{"toplevel": "t.drv", "formatScript": "f.drv", "mountScript": "m.drv"}
	if got := pick(drvs, "toplevel", "formatScript", "mountScript"); !slices.Equal(got, []string{"t.drv", "f.drv", "m.drv"}) {
		t.Errorf("got %v", got)
	}
	if got := pick(drvs, "", "mountScript"); !slices.Equal(got, []string{"m.drv"}) {
		t.Errorf("got %v", got)
	}
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
		{name: "ipv6", user: "root", host: "fe80::1", port: "22", root: "/mnt", want: "ssh://root@[fe80::1]?port=22&remote-store=local%3Froot%3D%2Fmnt"},
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
