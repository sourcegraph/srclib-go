package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/tools/go/vcs"

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

	// Check if this import path is in this tree. If refs refer to vendored deps, they are linked to the vendored code
	// inside this repository (i.e., NOT linked to the external repository from which the code was vendored).
	if pkg, err := buildContext.Import(importPath, "", build.FindOnly); err == nil {
		if pathHasPrefix(pkg.Dir, cwd) || isInEffectiveConfigGOPATH(pkg.Dir) {
			if name, isVendored := vendoredUnitName(pkg); isVendored {
				return &dep.ResolvedTarget{
					ToRepoCloneURL: "", // empty ToRepoCloneURL to indicate it's from this repository
					ToUnit:         name,
					ToUnitType:     "GoPackage",
				}, nil
			} else {
				return &dep.ResolvedTarget{
					ToRepoCloneURL: "", // empty ToRepoCloneURL to indicate it's from this repository

					ToUnit:     importPath,
					ToUnitType: "GoPackage",
				}, nil
			}
		}
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
		cloneURL, err := standardRepoHostImportPathToCloneURL(importPath)
		if err != nil {
			return nil, err
		}
		target.ToRepoCloneURL = cloneURL

	// Special-case google.golang.org/... (e.g., /appengine) import
	// paths for performance and to avoid hitting GitHub rate limit.
	case strings.HasPrefix(importPath, "google.golang.org/"):
		target.ToRepoCloneURL = "https://" + strings.Replace(importPath, "google.golang.org/", "github.com/golang/", 1) + ".git"
		target.ToUnit = strings.Replace(importPath, "google.golang.org/", "github.com/golang/", 1)

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
		target.ToUnit = strings.Replace(importPath, "golang.org/x/", "github.com/golang/", 1)

	// Try to resolve everything else
	default:
		log.Printf("Resolving Go dep: %s", importPath)
		repoRoot, err := vcs.RepoRootForImportPath(string(importPath), false)
		if err == nil {
			target.ToRepoCloneURL = repoRoot.Repo
			target.ToUnit = replaceImportPathRepoRoot(target.ToUnit, repoRoot.Root, repoRoot.Repo)
		} else {
			log.Printf("warning: unable to fetch information about Go package %q: %s", importPath, err)
			target.ToRepoCloneURL = importPath
		}
	}

	// Save in cache.
	resolveCache.Put(importPath, target)

	return target, nil
}

// standardRepoHostImportPathToCloneURL returns the clone URL for an
// import path that references a standard repo host (e.g.,
// github.com). It assumes a structure of
// $HOST/$OWNER/$REPO/$PACKAGE_PATH. E.g., "github.com/foo/bar/path/to/pkg".
func standardRepoHostImportPathToCloneURL(importPath string) (string, error) {
	parts := strings.SplitN(importPath, "/", 4)
	if len(parts) < 3 {
		return "", fmt.Errorf("import path expected to have at least 3 parts, but didn't: %q", importPath)
	}
	return "https://" + strings.Join(parts[:3], "/") + ".git", nil
}

// replaceImportPathRepoRoot modifies the given importPath by replacing the
// root string in the importPath with the host+path of the clone URL.
// This is necessary for resolving refs to custom import path packages, since
// the defs within those packages would have the Unit field set to
// "${repoURI}/path/to/pkg/dir", where repoURI is the host+path of the cloneURL.
//
// This is a HACK to make def resolution work in presence of custom import
// paths. A proper solution would require passing in the custom import path
// information from srclib to the graph step, and setting that as the Unit field
// of the defs identified in the source unit.
//
// TODO: Implement the proper solution and get rid of this hack.
func replaceImportPathRepoRoot(importPath, root, cloneURL string) string {
	i := strings.Index(cloneURL, "://")
	if i < 0 {
		return importPath
	}
	newRoot := strings.TrimSuffix(cloneURL[i+len("://"):], ".git")
	return strings.Replace(importPath, root, newRoot, 1)
}
