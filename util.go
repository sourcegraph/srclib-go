package main

import (
	"encoding/json"
	"go/build"
	"path/filepath"

	"sourcegraph.com/sourcegraph/srclib/unit"
)

func UnitDataAsBuildPackage(u *unit.SourceUnit) (*build.Package, error) {
	var pkg *build.Package
	if err := json.Unmarshal(u.Data, &pkg); err != nil {
		return nil, err
	}
	pkg.Dir = filepath.Join(cwd, pkg.Dir)
	return pkg, nil
}

func evalSymlinks(path string) string {
	newPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return newPath
}
