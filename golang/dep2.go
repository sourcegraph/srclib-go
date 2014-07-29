package golang

import (
	"fmt"
	"log"
	"net/http"
	"runtime"

	"strings"
	"sync"

	"github.com/golang/gddo/gosrc"
	"github.com/sourcegraph/srclib/dep"
)

var (
	resolveCache   map[string]*dep.ResolvedTarget
	resolveCacheMu sync.Mutex
)

func ResolveDep(importPath string, repoImportPath string) (*dep.ResolvedTarget, error) {
	// TODO(sqs): handle Go stdlib

	// Look up in cache.
	resolvedTarget := func() *dep.ResolvedTarget {
		resolveCacheMu.Lock()
		defer resolveCacheMu.Unlock()
		return resolveCache[importPath]
	}()
	if resolvedTarget != nil {
		return resolvedTarget, nil
	}

	// Check if this importPath is in this repository.
	if strings.HasPrefix(importPath, repoImportPath) {
		return &dep.ResolvedTarget{
			// empty ToRepoCloneURL to indicate it's from this repository
			ToRepoCloneURL: "",
			ToUnit:         importPath,
			ToUnitType:     "GoPackage",
		}, nil
	}

	// Special-case the cgo package "C".
	if importPath == "C" {
		return nil, nil
	}

	if gosrc.IsGoRepoPath(importPath) || importPath == "debug/goobj" || importPath == "debug/plan9obj" {
		return &dep.ResolvedTarget{
			ToRepoCloneURL:  "https://code.google.com/p/go",
			ToVersionString: runtime.Version(),
			ToRevSpec:       "", // TODO(sqs): fill in when graphing stdlib repo
			ToUnit:          importPath,
			ToUnitType:      "GoPackage",
		}, nil
	}

	// Special-case github.com/... import paths for performance.
	if strings.HasPrefix(importPath, "github.com/") {
		parts := strings.SplitN(importPath, "/", 4)
		if len(parts) < 3 {
			return nil, fmt.Errorf("import path starts with 'github.com/' but is not valid: %q", importPath)
		}
		return &dep.ResolvedTarget{
			ToRepoCloneURL: "https://" + strings.Join(parts[:3], "/") + ".git",
			ToUnit:         importPath,
			ToUnitType:     "GoPackage",
		}, nil
	}

	// Special-case code.google.com/p/... import paths for performance.
	if strings.HasPrefix(importPath, "code.google.com/p/") {
		parts := strings.SplitN(importPath, "/", 4)
		if len(parts) < 3 {
			return nil, fmt.Errorf("import path starts with 'code.google.com/p/' but is not valid: %q", importPath)
		}
		return &dep.ResolvedTarget{
			ToRepoCloneURL: "https://" + strings.Join(parts[:3], "/"),
			ToUnit:         importPath,
			ToUnitType:     "GoPackage",
		}, nil
	}

	log.Printf("Resolving Go dep: %s", importPath)

	dir, err := gosrc.Get(http.DefaultClient, string(importPath), "")
	if err != nil {
		if strings.Contains(err.Error(), "Git Repository is empty.") {
			// Not fatal, just weird.
			return nil, nil
		}
		return nil, fmt.Errorf("unable to fetch information about Go package %q: %s", importPath, err)
	}

	// gosrc returns code.google.com URLs ending in a slash. Remove it.
	dir.ProjectURL = strings.TrimSuffix(dir.ProjectURL, "/")

	resolvedTarget = &dep.ResolvedTarget{
		ToRepoCloneURL: dir.ProjectURL,
		ToUnit:         importPath,
		ToUnitType:     "GoPackage",
	}

	// Save in cache.
	resolveCacheMu.Lock()
	defer resolveCacheMu.Unlock()
	if resolveCache == nil {
		resolveCache = make(map[string]*dep.ResolvedTarget)
	}
	resolveCache[importPath] = resolvedTarget

	return resolvedTarget, nil
}
