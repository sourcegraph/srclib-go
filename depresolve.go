package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"log"
	"os"
	"strings"
	"sync"

	"sourcegraph.com/sourcegraph/srclib-go/depresolve"
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

	target, err := depresolve.ResolveImportPath(importPath)
	if err != nil {
		return nil, err
	}

	// Save in cache.
	resolveCache.Put(importPath, target)

	return target, nil
}
