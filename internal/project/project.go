package project

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/util"
)

// ProjectSource is a project that has been resolved and added to the nix store.
type ProjectSource struct {
	NillaPath string
	StorePath string
	StoreHash string
}

// FullProjectPath returns a full path to the directory containing the `nilla.nix`
// file in the project.
func (s *ProjectSource) FullProjectPath() string {
	return filepath.Dir(s.FullNillaPath())
}

// FullNillaPath returns a full path to the `nilla.nix` file in the project.
func (s *ProjectSource) FullNillaPath() string {
	return filepath.Clean(filepath.Join(s.StorePath, s.NillaPath))
}

// FixedOutputStoreEntry returns a `*nix.FixedOutputStoreEntry` for the base of
// the nilla project.
func (s *ProjectSource) FixedOutputStoreEntry() *nix.FixedOutputStoreEntry {
	return &nix.FixedOutputStoreEntry{
		Path: s.StorePath,
		Hash: s.StoreHash,
	}
}

// Resolve takes in a project uri and tries to resolve it into a nix store path.
// Currently supported URIs:
//   - Paths starting with `./`, `/` or `~`
//   - URI starting with `path:`
//   - URI starting with `github:` (example: `github:owner/repo`)
//   - URI starting with `gitlab:` (example: `gitlab:owner/repo`)
func Resolve(uri string) (*ProjectSource, error) {
	var source *ProjectSource

	log.Infof("Looking for project at %s", uri)

	switch {
	// Standard path
	case isPath(uri):
		resolved, err := ResolvePath(uri)
		if err != nil {
			return nil, err
		}
		source = resolved

	// With `path:` prefix
	case strings.HasPrefix(uri, "path:"):
		resolved, err := ResolvePath(strings.TrimPrefix(uri, "path:"))
		if err != nil {
			return nil, err
		}
		source = resolved

	// With `github:` prefix
	case strings.HasPrefix(uri, "github:"):
		resolved, err := ResolveGithub(uri)
		if err != nil {
			return nil, err
		}
		source = resolved

	// With `gitlab:` prefix
	case strings.HasPrefix(uri, "gitlab:"):
		resolved, err := ResolveGitlab(uri)
		if err != nil {
			return nil, err
		}
		source = resolved
	}

	if source != nil {
		log.Debugf("Resolved project \"%s\"", source.FullProjectPath())
		return source, nil
	}

	return nil, fmt.Errorf("Could not parse URL scheme for %s", uri)
}

// ResolvePath resolves a path based project uri and loads it into the nix store.
func ResolvePath(path string) (*ProjectSource, error) {
	// Strip nilla.nix suffix, if provided
	if strings.HasSuffix(path, "nilla.nix") {
		path = filepath.Dir(path)
	}

	// Resolve tilde and relative path
	fullpath, err := resolveRealPath(path)
	if err != nil {
		return nil, err
	}

	// Look up the directory tree for nilla.nix
	resolved, err := searchUpForNillaNix(fullpath)
	if err != nil {
		return nil, err
	}

	// Check if we're in a git repository
	if root, err := searchUpForGitDir(resolved); err == nil {
		log.Debugf("Found git repository root at %s", root)
		return resolveGitPath(root, resolved)
	}

	log.Debugf("Found path %s", resolved)

	// Add resolved path to nix store
	entry, err := nix.AddPathToStore(resolved)
	if err != nil {
		return nil, err
	}

	return &ProjectSource{
		NillaPath: "./nilla.nix",
		StorePath: entry.Path,
		StoreHash: entry.Hash,
	}, nil
}

// ResolveGithub resolves a uri starting with `github:` and loads it into the nix store
// from a github repository.
func ResolveGithub(uri string) (*ProjectSource, error) {
	// Parse as url
	gitURL, err := url.Parse(fmt.Sprintf("github://%s", strings.TrimPrefix(uri, "github:")))
	if err != nil {
		return nil, err
	}

	return resolveGenericGit(uri, gitURL, "github.com")
}

// ResolveGitlab resolves a uri starting with `gitlab:` and loads it into the nix store
// form a gitlab repository.
func ResolveGitlab(uri string) (*ProjectSource, error) {
	// Parse as url
	gitURL, err := url.Parse(fmt.Sprintf("gitlab://%s", strings.TrimPrefix(uri, "gitlab:")))
	if err != nil {
		return nil, err
	}

	return resolveGenericGit(uri, gitURL, "gitlab.com")
}

func resolveGenericGit(uri string, gitURL *url.URL, defaultHost string) (*ProjectSource, error) {
	// Parse various fields from url
	owner := gitURL.Host
	paths := strings.Split(strings.TrimPrefix(gitURL.Path, "/"), "/")
	if len(paths) < 1 {
		return nil, fmt.Errorf("Project URL \"%s\" missing repository", uri)
	}
	repo := paths[0]

	// Parse query options
	rev := gitURL.Query().Get("rev")
	ref := gitURL.Query().Get("ref")
	submodules := false
	if sub, err := strconv.ParseBool(gitURL.Query().Get("submodules")); err == nil {
		submodules = sub
	}
	host := defaultHost
	if gitURL.Query().Has("host") {
		host = gitURL.Query().Get("host")
	}

	// Fetch git repo
	fetchURL := fmt.Sprintf("git@%s:%s/%s.git", host, owner, repo)
	log.Debugf("Fetching \"%s\"", fetchURL)
	entry, err := nix.FetchGit(
		fetchURL,
		&nix.FetchGitOptions{
			Revision:   rev,
			Reference:  ref,
			Submodules: submodules,
		},
	)
	if err != nil {
		return nil, err
	}

	// Check dir
	nilla := "./nilla.nix"
	if gitURL.Query().Has("dir") {
		nilla = filepath.Join(gitURL.Query().Get("dir"), "nilla.nix")
	}

	if info, err := os.Stat(filepath.Join(entry.Path, nilla)); err != nil || info.IsDir() {
		return nil, fmt.Errorf("Directory \"%s\" does not contain a nilla.nix", filepath.Dir(nilla))
	}

	return &ProjectSource{
		NillaPath: nilla,
		StorePath: entry.Path,
		StoreHash: entry.Hash,
	}, nil
}

func isPath(uri string) bool {
	return strings.HasPrefix(uri, ".") ||
		strings.HasPrefix(uri, "/") ||
		strings.HasPrefix(uri, "~")
}

func resolveRealPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		path = filepath.Join(util.GetHomeDir(), strings.TrimPrefix(path[1:], "/"))
	}

	return filepath.Abs(path)
}

func searchUpForNillaNix(path string) (string, error) {
	for {
		_, err := os.Stat(filepath.Join(path, "nilla.nix"))
		if err == nil {
			return path, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		if dir := filepath.Dir(path); dir != path {
			path = dir
			continue
		}

		return "", fmt.Errorf("could not find nilla.nix in %s", path)
	}
}

func searchUpForGitDir(path string) (string, error) {
	for {
		info, err := os.Stat(filepath.Join(path, ".git"))
		if err == nil && info.IsDir() {
			return path, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		if dir := filepath.Dir(path); dir != path {
			path = dir
			continue
		}

		return "", fmt.Errorf("could not find a git directory in %s", path)
	}
}

func resolveGitPath(root, path string) (*ProjectSource, error) {
	// Get untracked files
	untracked := getUntrackedFiles(path)
	if len(untracked) > 0 {
		log.Warnf("Untracked files in \"%s\" will not be available within Nix", path)
		for _, f := range untracked {
			log.Warnf("  %s", f)
		}
		log.Warn("If you experience issues, try adding these files to your git repository with `git add`")
	}

	// Add git path to nix store
	entry, err := nix.AddGitPathToStore(root)
	if err != nil {
		return nil, err
	}

	// Strip root prefix from path
	stripped := strings.TrimPrefix(path, root)

	return &ProjectSource{
		NillaPath: filepath.Join("./", stripped, "nilla.nix"),
		StorePath: entry.Path,
		StoreHash: entry.Hash,
	}, nil
}

func getUntrackedFiles(path string) []string {
	// Check if git is in path
	gitp, err := exec.LookPath("git")
	if err != nil {
		return []string{}
	}

	// Run git ls-files
	cmd := exec.Command(
		gitp, "ls-files", "--others", "--exclude-standard",
	)
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return []string{}
	}

	// Split list by lines
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	return slices.DeleteFunc(lines, func(line string) bool {
		return line == ""
	})
}
