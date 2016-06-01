package main

import (
	"fmt"
	"go/build"
	"log"
	"strings"
	"sync"

	"sourcegraph.com/sourcegraph/srclib-go/depresolve"
	"sourcegraph.com/sourcegraph/srclib/dep"
)

func init() {
	_, err := flagParser.AddCommand("depresolve",
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
	fmt.Println("[]")
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

	target, err := doResolveDep(importPath)
	if err != nil {
		return nil, err
	}

	// Save in cache.
	resolveCache.Put(importPath, target)

	return target, nil
}

func doResolveDep(importPath string) (*dep.ResolvedTarget, error) {
	// Check if this import path is in this tree. If refs refer to vendored deps, they are linked to the vendored code
	// inside this repository (i.e., NOT linked to the external repository from which the code was vendored).
	if pkg, err := buildContext.Import(strings.TrimSuffix(importPath, "_test"), "", build.FindOnly); err == nil {
		if pathHasPrefix(pkg.Dir, cwd) {
			if name, isVendored := vendoredUnitName(pkg); isVendored {
				if strings.HasSuffix(importPath, "_test") {
					name += "_test"
				}
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

	return depresolve.ResolveImportPath(importPath)
}
