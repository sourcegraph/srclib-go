package main

import (
	"encoding/json"
	"go/build"
	"path/filepath"

	"sourcegraph.com/sourcegraph/srclib/unit"
)

func UnitDataAsBuildPackage(u *unit.SourceUnit) (*build.Package, error) {
	data, err := json.Marshal(u.Data)
	if err != nil {
		return nil, err
	}

	var pkg *build.Package
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

func evalSymlinks(path string) string {
	newPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return newPath
}
