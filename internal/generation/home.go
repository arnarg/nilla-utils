package generation

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/arnarg/nilla-utils/internal/util"
)

// localHomeProfileDirs returns the list of profile directories to check for local operations.
func localHomeProfileDirs() []string {
	var dirs []string
	if user := util.GetUser(); user != "" {
		dirs = append(dirs, fmt.Sprintf("/nix/var/nix/profiles/per-user/%s", user))
	}
	if home := util.GetHomeDir(); home != "" {
		dirs = append(dirs, fmt.Sprintf("%s/.local/state/nix/profiles", home))
	}
	return dirs
}

// homeProfileDirsFor returns the list of profile directories to check for executor-aware operations.
func homeProfileDirsFor(user string, homeResolver homeDirResolver) ([]string, error) {
	var dirs []string
	if user != "" {
		dirs = append(dirs, fmt.Sprintf("/nix/var/nix/profiles/per-user/%s", user))
	}
	homeDir, err := homeResolver.getHomeDir(user)
	if err == nil && homeDir != "" {
		dirs = append(dirs, fmt.Sprintf("%s/.local/state/nix/profiles", homeDir))
	}
	return dirs, nil
}

// findCurrentHomeGenerationPath is the shared helper that finds the current Home Manager generation path.
// It checks both /nix/var/nix/profiles/per-user/<user> and ~/.local/state/nix/profiles locations.
// Two layouts are supported:
//   1) Standalone Home Manager:   home-manager -> home-manager-<id>-link
//   2) NixOS module integration:  profile      -> profile-<id>-link   (takes precedence if present)
func findCurrentHomeGenerationPath(user string, homeResolver homeDirResolver, linkReader linkReader) (string, error) {
	dirs, err := homeProfileDirsFor(user, homeResolver)
	if err != nil {
		return "", err
	}

	for _, dir := range dirs {
		// Prefer NixOS module-style layout: profile -> profile-<id>-link
		profileLink := fmt.Sprintf("%s/profile", dir)
		exists, err := linkReader.pathExists(profileLink)
		if err == nil && exists {
			linkName, err := linkReader.readLink(profileLink)
			if err == nil {
				return fmt.Sprintf("%s/%s", dir, linkName), nil
			}
		}

		// Fallback to standalone Home Manager layout: home-manager -> home-manager-<id>-link
		homeManagerLink := fmt.Sprintf("%s/home-manager", dir)
		exists, err = linkReader.pathExists(homeManagerLink)
		if err == nil && exists {
			linkName, err := linkReader.readLink(homeManagerLink)
			if err == nil {
				return fmt.Sprintf("%s/%s", dir, linkName), nil
			}
		}
	}

	return "", errors.New("current generation not found")
}

// CurrentHomeGeneration returns the current Home Manager generation using the given executor.
// If executor is nil or local, uses local filesystem. If username is provided, uses it for remote queries.
func CurrentHomeGeneration(e exec.Executor, username string) (*HomeGeneration, error) {
	user := username
	if user == "" {
		user = util.GetUser()
	}

	homeResolver := createHomeDirResolver(e)
	linkReader, dirReader, fileReader := createReaders(e)

	path, err := findCurrentHomeGenerationPath(user, homeResolver, linkReader)
	if err != nil {
		return nil, err
	}

	// Convert path to HomeGeneration struct
	return pathToHomeGeneration(path, dirReader, fileReader, linkReader)
}

// CurrentHomeGenerationPath queries the current Home Manager generation path using the given executor.
// If username is provided, it will be used to query the remote home directory.
// It returns the path and true if found, or empty string and false if not found.
func CurrentHomeGenerationPath(e exec.Executor, username string) (string, bool) {
	user := username
	if user == "" {
		user = util.GetUser()
	}

	homeResolver := createHomeDirResolver(e)
	linkReader, _, _ := createReaders(e)

	path, err := findCurrentHomeGenerationPath(user, homeResolver, linkReader)
	if err != nil {
		return "", false
	}

	return path, true
}

// pathToHomeGeneration converts a generation path string to a HomeGeneration struct.
// This works with both local and remote executors.
func pathToHomeGeneration(path string, dirReader dirReader, fileReader fileReader, linkReader linkReader) (*HomeGeneration, error) {
	// Extract directory from path
	dir := filepath.Dir(path)

	// Get file info
	info, err := dirReader.stat(path)
	if err != nil {
		return nil, err
	}

	return NewHomeGeneration(dir, info, fileReader, linkReader)
}

type HomeGeneration struct {
	ID        int
	BuildDate time.Time
	Version   string

	path string
}

func NewHomeGeneration(root string, info fs.FileInfo, fileReader fileReader, linkReader linkReader) (*HomeGeneration, error) {
	// Get ID from name. Support both standalone and NixOS module layouts:
	//   home-manager-<id>-link  (standalone)
	//   profile-<id>-link       (NixOS module)
	matches := regexp.MustCompile(`^(?:home-manager|profile)-(\d+)-link$`).FindStringSubmatch(info.Name())
	if len(matches) < 2 || matches[1] == "" {
		return nil, fmt.Errorf("generation path '%s' does not match supported patterns", info.Name())
	}
	id, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, err
	}

	// Build full path
	path := fmt.Sprintf("%s/%s", root, info.Name())

	// Determine if this is a NixOS module generation (profile-*-link) or standalone (home-manager-*-link)
	isNixOSModule := strings.HasPrefix(info.Name(), "profile-")

	// Read version: "from NixOS" for NixOS module, hm-version for standalone
	var version string
	if isNixOSModule {
		// NixOS module generation: set version to "from NixOS"
		version = "from NixOS"
	} else {
		// Standalone Home Manager generation: read hm-version
		homeVer, err := fileReader.readFile(fmt.Sprintf("%s/hm-version", path))
		if err != nil {
			// If hm-version doesn't exist, use empty string
			version = ""
		} else {
			version = string(bytes.TrimSpace(homeVer))
		}
	}

	return &HomeGeneration{
		ID:        id,
		BuildDate: info.ModTime(),
		Version:   version,
		path:      path,
	}, nil
}

func (g *HomeGeneration) Delete() error {
	return g.DeleteWithExecutor(nil)
}

func (g *HomeGeneration) DeleteWithExecutor(e exec.Executor) error {
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

func (g *HomeGeneration) Path() string {
	return g.path
}

// ListHomeGenerations lists all Home Manager generations using the given executor.
// If executor is nil or local, uses local filesystem. If username is provided, uses it for remote queries.
func ListHomeGenerations(e exec.Executor, username string) ([]*HomeGeneration, error) {
	user := username
	if user == "" {
		user = util.GetUser()
	}

	homeResolver := createHomeDirResolver(e)
	linkReader, dirReader, fileReader := createReaders(e)

	dirs, err := homeProfileDirsFor(user, homeResolver)
	if err != nil {
		return nil, err
	}

	for _, dir := range dirs {
		gens, err := listHomeGenerations(dir, dirReader, fileReader, linkReader)
		if err != nil {
			// If directory doesn't exist, continue to next
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if len(gens) > 0 {
			return gens, nil
		}
	}

	return []*HomeGeneration{}, nil
}

func listHomeGenerations(dir string, dirReader dirReader, fileReader fileReader, linkReader linkReader) ([]*HomeGeneration, error) {
	// List files in dir
	entries, err := dirReader.readDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*HomeGeneration{}, nil
		}
		return nil, err
	}

	// Separate NixOS-module-style and standalone Home Manager generations.
	// If any profile-<id>-link entries exist, they take precedence.
	profileRegex := regexp.MustCompile(`^profile-\d+-link$`)
	homeRegex := regexp.MustCompile(`^home-manager-\d+-link$`)

	profileEntries := []fs.DirEntry{}
	homeEntries := []fs.DirEntry{}

	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink == 0 {
			continue
		}
		switch {
		case profileRegex.MatchString(entry.Name()):
			profileEntries = append(profileEntries, entry)
		case homeRegex.MatchString(entry.Name()):
			homeEntries = append(homeEntries, entry)
		}
	}

	// Choose which set of entries to use. NixOS module (profile-*) supersedes standalone.
	candidates := profileEntries
	if len(candidates) == 0 {
		candidates = homeEntries
	}

	generations := []*HomeGeneration{}
	for _, entry := range candidates {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}

		generation, err := NewHomeGeneration(dir, info, fileReader, linkReader)
		if err != nil {
			return nil, err
		}

		generations = append(generations, generation)
	}

	return generations, nil
}
