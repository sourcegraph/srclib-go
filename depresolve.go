package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/golang/gddo/gosrc"

	"sourcegraph.com/sourcegraph/srclib/dep"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

func init() {
	_, err := parser.AddCommand("depresolve",
		"resolve a Go package's imports",
		"Resolve a Go package's imports to their repository clone URL.",
		&depResolveCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

type DepResolveCmd struct {
	Config []string `long:"config" description:"config property from Srcfile" value-name:"KEY=VALUE"`
}

var depResolveCmd DepResolveCmd

func (c *DepResolveCmd) Execute(args []string) error {
	var unit *unit.SourceUnit
	if err := json.NewDecoder(os.Stdin).Decode(&unit); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}

	if err := unmarshalTypedConfig(unit.Config); err != nil {
		return err
	}
	if err := config.apply(); err != nil {
		return err
	}

	res := make([]*dep.Resolution, len(unit.Dependencies))
	for i, rawDep := range unit.Dependencies {
		importPath, ok := rawDep.(string)
		if !ok {
			return fmt.Errorf("Go raw dep is not a string import path: %v (%T)", rawDep, rawDep)
		}

		res[i] = &dep.Resolution{Raw: rawDep}

		rt, err := ResolveDep(importPath)
		if err != nil {
			res[i].Error = err.Error()
			continue
		}
		res[i].Target = rt
	}

	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(b); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// targetCache caches (dep).ResolvedTarget's for importPaths
type targetCache struct {
	data map[string]*dep.ResolvedTarget
	mu   sync.Mutex
}

// Get returns the cached (dep).ResolvedTarget for the given import path or nil.
func (t *targetCache) Get(path string) *dep.ResolvedTarget {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.data[path]
}

// Put puts a new entry into the cache at the specified import path.
func (t *targetCache) Put(path string, target *dep.ResolvedTarget) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.data == nil {
		t.data = make(map[string]*dep.ResolvedTarget)
	}
	t.data[path] = target
}

var resolveCache targetCache

func ResolveDep(importPath string) (*dep.ResolvedTarget, error) {
	// Look up in cache.
	if target := resolveCache.Get(importPath); target != nil {
		return target, nil
	}
	if strings.HasSuffix(importPath, "_test") {
		// TODO(sqs): handle xtest packages - these should not be appearing here
		// as import paths, but they are, so suppress errors
		return nil, fmt.Errorf("xtest package (%s) is not yet supported", importPath)
	}

	// Check if this import path is in this tree.
	if pkg, err := buildContext.Import(importPath, "", build.FindOnly); err == nil && (pathHasPrefix(pkg.Dir, cwd) || isInEffectiveConfigGOPATH(pkg.Dir)) {
		// TODO(sqs): do we want to link refs to vendored deps to
		// their vendored code inside this repo? that's what it's
		// doing now. The alternative is to link to the external repo
		// that the code was vendored from.
		return &dep.ResolvedTarget{
			// empty ToRepoCloneURL to indicate it's from this repository
			ToRepoCloneURL: "",
			ToUnit:         importPath,
			ToUnitType:     "GoPackage",
		}, nil
	}

	// Handle some special (and edge) cases faster for performance and corner-cases.
	target := &dep.ResolvedTarget{ToUnit: importPath, ToUnitType: "GoPackage"}
	switch {
	// CGO package "C"
	case importPath == "C":
		return nil, nil

	// Go standard library packages
	case gosrc.IsGoRepoPath(importPath) || strings.HasPrefix(importPath, "debug/") || strings.HasPrefix(importPath, "cmd/"):
		target.ToRepoCloneURL = "https://github.com/golang/go"
		target.ToVersionString = runtime.Version()
		target.ToRevSpec = "" // TODO(sqs): fill in when graphing stdlib repo

	// Special-case github.com/... import paths for performance.
	case strings.HasPrefix(importPath, "github.com/") || strings.HasPrefix(importPath, "sourcegraph.com/"):
		parts := strings.SplitN(importPath, "/", 4)
		if len(parts) < 3 {
			return nil, fmt.Errorf("import path starts with '(github|sourcegraph).com/' but is not valid: %q", importPath)
		}
		target.ToRepoCloneURL = "https://" + strings.Join(parts[:3], "/") + ".git"

	// Special-case google.golang.org/... (e.g., /appengine) import
	// paths for performance and to avoid hitting GitHub rate limit.
	case strings.HasPrefix(importPath, "google.golang.org/"):
		target.ToRepoCloneURL = "https://" + strings.Replace(importPath, "google.golang.org/", "github.com/golang/", 1) + ".git"

	// Special-case code.google.com/p/... import paths for performance.
	case strings.HasPrefix(importPath, "code.google.com/p/"):
		parts := strings.SplitN(importPath, "/", 4)
		if len(parts) < 3 {
			return nil, fmt.Errorf("import path starts with 'code.google.com/p/' but is not valid: %q", importPath)
		}
		target.ToRepoCloneURL = "https://" + strings.Join(parts[:3], "/")

	// Special-case golang.org/x/... import paths for performance.
	case strings.HasPrefix(importPath, "golang.org/x/"):
		parts := strings.SplitN(importPath, "/", 4)
		if len(parts) < 3 {
			return nil, fmt.Errorf("import path starts with 'golang.org/x/' but is not valid: %q", importPath)
		}
		target.ToRepoCloneURL = "https://" + strings.Replace(strings.Join(parts[:3], "/"), "golang.org/x/", "github.com/golang/", 1)

	// Try to resolve everything else
	default:
		log.Printf("Resolving Go dep: %s", importPath)
		dir, err := gosrc.Get(http.DefaultClient, string(importPath), "")
		if err == nil {
			target.ToRepoCloneURL = strings.TrimSuffix(dir.ProjectURL, "/")
		} else {
			log.Printf("warning: unable to fetch information about Go package %q: %s", importPath, err)
			target.ToRepoCloneURL = importPath
		}
	}

	// Save in cache.
	resolveCache.Put(importPath, target)

	return target, nil
}
