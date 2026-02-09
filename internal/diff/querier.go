package diff

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/arnarg/nilla-utils/internal/exec"
	"github.com/valyala/fastjson"
)

var storePathRegex = regexp.MustCompile(`^/nix/store/[a-z0-9]+-(.+?)(?:-([0-9].*?))?(?:\.drv)?$`)

// StoreQuerier abstracts Nix store operations.
type StoreQuerier interface {
	QueryPackages(ctx context.Context, generationPath string) ([]Package, error)
	GetClosureSize(ctx context.Context, generationPath string) (int64, error)
}

type executorQuerier struct {
	exec exec.Executor
}

// NewExecutorQuerier returns a StoreQuerier that uses an executor to run nix CLI.
func NewExecutorQuerier(e exec.Executor) StoreQuerier {
	return &executorQuerier{exec: e}
}

func (q *executorQuerier) QueryPackages(ctx context.Context, path string) ([]Package, error) {
	paths, err := q.resolveClosurePaths(ctx, path)
	if err != nil {
		return nil, err
	}

	var packages []Package
	seen := make(map[string]struct{})

	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}

		if pkg := parsePackageFromPath(p); pkg != nil {
			packages = append(packages, *pkg)
		}
	}

	return packages, nil
}

func (q *executorQuerier) GetClosureSize(ctx context.Context, path string) (int64, error) {
	buf := &bytes.Buffer{}
	cmd, err := q.exec.Command("nix", "path-info", "--json", "--closure-size", path)
	if err != nil {
		return 0, fmt.Errorf("create command: %w", err)
	}
	cmd.SetStdout(buf)

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("nix path-info failed: %w", err)
	}

	return decodeClosureSize(buf.Bytes())
}

func (q *executorQuerier) resolveClosurePaths(ctx context.Context, path string) ([]string, error) {
	swPath := path + "/sw"
	swExists, err := q.exec.PathExists(swPath)
	if err != nil {
		return nil, fmt.Errorf("check path exists: %w", err)
	}

	var allPaths []string

	if swExists {
		// Try /sw first; if query fails, fall back to base path
		refs, err := q.queryReferences(swPath)
		if err == nil {
			reqs, _ := q.queryRequisites(path)
			allPaths = append(allPaths, splitLines(refs)...)
			allPaths = append(allPaths, splitLines(reqs)...)
			return allPaths, nil
		}
	}

	// Fallback: query base path directly
	reqs, err := q.queryRequisites(path)
	if err != nil {
		return nil, fmt.Errorf("query requisites: %w", err)
	}
	allPaths = append(allPaths, splitLines(reqs)...)
	return allPaths, nil
}

func (q *executorQuerier) queryReferences(path string) ([]byte, error) {
	return q.runNixStore("--query", "--references", path)
}

func (q *executorQuerier) queryRequisites(path string) ([]byte, error) {
	return q.runNixStore("--query", "--requisites", path)
}

func (q *executorQuerier) runNixStore(args ...string) ([]byte, error) {
	buf := &bytes.Buffer{}
	cmd, err := q.exec.Command("nix-store", args...)
	if err != nil {
		return nil, err
	}
	cmd.SetStdout(buf)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parsePackageFromPath(path string) *Package {
	matches := storePathRegex.FindStringSubmatch(path)
	if len(matches) < 2 {
		return nil
	}
	ver := ""
	if len(matches) > 2 {
		ver = matches[2]
	}
	return &Package{
		Name:    PackageName(matches[1]),
		Version: Version(ver),
		Path:    path,
	}
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func decodeClosureSize(buf []byte) (int64, error) {
	val, err := fastjson.ParseBytes(buf)
	if err != nil {
		return 0, fmt.Errorf("parse json: %w", err)
	}

	switch val.Type() {
	case fastjson.TypeArray:
		arr := val.GetArray()
		if len(arr) == 0 {
			return 0, nil
		}
		return arr[0].GetInt64("closureSize"), nil

	case fastjson.TypeObject:
		obj := val.GetObject()
		var key string
		obj.Visit(func(k []byte, v *fastjson.Value) {
			if key == "" {
				key = string(k)
			}
		})
		if key == "" {
			return 0, nil
		}
		gen := obj.Get(key)
		if gen == nil {
			return 0, nil
		}
		return gen.GetInt64("closureSize"), nil
	}

	return 0, nil
}
