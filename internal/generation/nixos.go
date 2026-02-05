package generation

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
)

const PROFILES_DIR = "/nix/var/nix/profiles"

// CurrentNixOSGeneration returns the current NixOS generation using the given executor.
// If executor is nil or local, uses local filesystem.
func CurrentNixOSGeneration(e exec.Executor) (*NixOSGeneration, error) {
	p := fmt.Sprintf("%s/system", PROFILES_DIR)

	linkReader, dirReader, fileReader := createReaders(e)

	// Check if system link exists
	exists, err := linkReader.pathExists(p)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("system profile does not exist")
	}

	// Resolve link
	res, err := linkReader.readLink(p)
	if err != nil {
		return nil, err
	}

	// Get absolute path
	absp := fmt.Sprintf("%s/%s", PROFILES_DIR, res)
	if !filepath.IsAbs(absp) {
		absp = filepath.Join(PROFILES_DIR, res)
	}

	// Get file info on resolved link
	linfo, err := dirReader.stat(absp)
	if err != nil {
		return nil, err
	}

	return NewNixOSGeneration(filepath.Dir(absp), linfo, fileReader, e)
}

type NixOSGeneration struct {
	ID            int
	BuildDate     time.Time
	Version       string
	KernelVersion string

	path string
}

func NewNixOSGeneration(root string, info fs.FileInfo, fileReader fileReader, e exec.Executor) (*NixOSGeneration, error) {
	// Get ID from name
	strID := regexp.MustCompile(`^system-(\d+)-link$`).FindStringSubmatch(info.Name())
	if strID[1] == "" {
		return nil, fmt.Errorf("generation path '%s' does not match pattern", info.Name())
	}
	id, err := strconv.Atoi(strID[1])
	if err != nil {
		return nil, err
	}

	// Build full path
	path := fmt.Sprintf("%s/%s", root, info.Name())

	// Read NixOS version
	nixosVer, err := fileReader.readFile(fmt.Sprintf("%s/nixos-version", path))
	if err != nil {
		return nil, err
	}
	version := string(bytes.TrimSpace(nixosVer))

	// Get kernel version
	kernelVer, err := getKernelVersionWithExecutor(path, e)
	if err != nil {
		return nil, err
	}

	return &NixOSGeneration{
		ID:            id,
		BuildDate:     info.ModTime(),
		Version:       version,
		KernelVersion: kernelVer,
		path:          path,
	}, nil
}

func (g *NixOSGeneration) Delete() error {
	return g.DeleteWithExecutor(nil)
}

func (g *NixOSGeneration) DeleteWithExecutor(e exec.Executor) error {
	deleter := createDeleter(e)

	if err := deleter.delete(g.path); err != nil {
		// Check if it's a "not exist" error
		if e != nil && !e.IsLocal() {
			// For remote, check if file exists
			exists, _ := e.PathExists(g.path)
			if !exists {
				return nil // File already deleted
			}
		}
		return err
	}
	return nil
}

func (g *NixOSGeneration) Path() string {
	return g.path
}

func getKernelVersion(system string) (string, error) {
	return getKernelVersionWithExecutor(system, nil)
}

func getKernelVersionWithExecutor(system string, e exec.Executor) (string, error) {
	kernelModulesPath := fmt.Sprintf("%s/kernel-modules/lib/modules", system)

	_, dirReader, _ := createReaders(e)

	// List directories in <system>/kernel-modules/lib/modules
	entries, err := dirReader.readDir(kernelModulesPath)
	if err != nil {
		return "Unknown", nil // If kernel modules don't exist, return Unknown
	}

	// Find the first sub-folder matching semver
	for _, entry := range entries {
		if regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(entry.Name()) {
			return strings.TrimSuffix(entry.Name(), "/"), nil
		}
	}

	return "Unknown", nil
}

// ListNixOSGenerations lists all NixOS generations using the given executor.
// If executor is nil or local, uses local filesystem.
func ListNixOSGenerations(e exec.Executor) ([]*NixOSGeneration, error) {
	_, dirReader, fileReader := createReaders(e)

	// List files in root
	entries, err := dirReader.readDir(PROFILES_DIR)
	if err != nil {
		return nil, err
	}

	// Iterate over entries and build list of generations
	generations := []*NixOSGeneration{}
	regex := regexp.MustCompile(`^system-\d+-link$`)
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 && regex.MatchString(entry.Name()) {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}

			generation, err := NewNixOSGeneration(PROFILES_DIR, info, fileReader, e)
			if err != nil {
				return nil, err
			}

			generations = append(generations, generation)
		}
	}

	return generations, nil
}
