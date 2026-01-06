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
func findCurrentHomeGenerationPath(user string, homeResolver homeDirResolver, linkReader linkReader) (string, error) {
	dirs, err := homeProfileDirsFor(user, homeResolver)
	if err != nil {
		return "", err
	}

	for _, dir := range dirs {
		homeManagerLink := fmt.Sprintf("%s/home-manager", dir)
		exists, err := linkReader.pathExists(homeManagerLink)
		if err != nil {
			continue
		}
		if exists {
			linkName, err := linkReader.readLink(homeManagerLink)
			if err == nil {
				return fmt.Sprintf("%s/%s", dir, linkName), nil
			}
		}
	}

	return "", errors.New("current generation not found")
}

// CurrentHomeGeneration returns the current Home Manager generation using local filesystem operations.
func CurrentHomeGeneration() (*HomeGeneration, error) {
	user := util.GetUser()
	homeResolver := &localHomeDirResolver{}
	linkReader := &localLinkReader{}

	path, err := findCurrentHomeGenerationPath(user, homeResolver, linkReader)
	if err != nil {
		return nil, err
	}

	// Convert path to HomeGeneration struct
	return pathToHomeGeneration(path)
}

// CurrentHomeGenerationPath queries the current Home Manager generation path using the given executor.
// If username is provided, it will be used to query the remote home directory.
// It returns the path and true if found, or empty string and false if not found.
func CurrentHomeGenerationPath(e exec.Executor, username string) (string, bool) {
	user := username
	if user == "" {
		user = util.GetUser()
	}

	var homeResolver homeDirResolver
	var linkReader linkReader

	if e.IsLocal() {
		homeResolver = &localHomeDirResolver{}
		linkReader = &localLinkReader{}
	} else {
		homeResolver = &remoteHomeDirResolver{executor: e}
		linkReader = &remoteLinkReader{executor: e}
	}

	path, err := findCurrentHomeGenerationPath(user, homeResolver, linkReader)
	if err != nil {
		return "", false
	}

	return path, true
}

// pathToHomeGeneration converts a generation path string to a HomeGeneration struct.
// This is used for local operations where we need the full struct with metadata.
func pathToHomeGeneration(path string) (*HomeGeneration, error) {
	// Extract directory from path
	dir := filepath.Dir(path)

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	return NewHomeGeneration(dir, info)
}

type HomeGeneration struct {
	ID        int
	BuildDate time.Time
	Version   string

	path string
}

func NewHomeGeneration(root string, info fs.FileInfo) (*HomeGeneration, error) {
	// Get ID from name
	strID := regexp.MustCompile(`^home-manager-(\d+)-link$`).FindStringSubmatch(info.Name())
	if strID[1] == "" {
		return nil, fmt.Errorf("generation path '%s' does not match pattern", info.Name())
	}
	id, err := strconv.Atoi(strID[1])
	if err != nil {
		return nil, err
	}

	// Build full path
	path := fmt.Sprintf("%s/%s", root, info.Name())

	// Read home-manager version
	homeVer, err := os.ReadFile(fmt.Sprintf("%s/hm-version", path))
	if err != nil {
		return nil, err
	}

	return &HomeGeneration{
		ID:        id,
		BuildDate: info.ModTime(),
		Version:   string(bytes.TrimSpace(homeVer)),
		path:      path,
	}, nil
}

func (g *HomeGeneration) Delete() error {
	if err := os.Remove(g.path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func (g *HomeGeneration) Path() string {
	return g.path
}

func ListHomeGenerations() ([]*HomeGeneration, error) {
	dirs := localHomeProfileDirs()
	for _, dir := range dirs {
		gens, err := listHomeGenerations(dir)
		if err != nil {
			return nil, err
		}
		if len(gens) > 0 {
			return gens, nil
		}
	}

	return []*HomeGeneration{}, nil
}

func listHomeGenerations(dir string) ([]*HomeGeneration, error) {
	// List files in dir
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*HomeGeneration{}, nil
		}
		return nil, err
	}

	// Iterate over entries and build list of generations
	generations := []*HomeGeneration{}
	regex := regexp.MustCompile(`^home-manager-\d+-link$`)
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 && regex.MatchString(e.Name()) {
			info, err := e.Info()
			if err != nil {
				return nil, err
			}

			generation, err := NewHomeGeneration(dir, info)
			if err != nil {
				return nil, err
			}

			generations = append(generations, generation)
		}
	}

	return generations, nil
}
