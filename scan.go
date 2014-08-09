package main

import (
	"encoding/json"
	"go/build"
	"log"
	"os"
	"path/filepath"

	"sourcegraph.com/sourcegraph/srclib-go/golang"
)

type ScanCmd struct {
	Repo   string   `long:"repo" description:"repository URI" value-name:"URI"`
	Subdir string   `long:"subdir" description:"subdirectory in repository" value-name:"DIR"`
	Config []string `long:"config" description:"config property from Srcfile" value-name:"KEY=VALUE"`
}

var scanCmd ScanCmd

func (c *ScanCmd) Execute(args []string) error {
	if c.Repo == "" && os.Getenv("IN_DOCKER_CONTAINER") != "" {
		log.Println("Warning: no --repo specified, and tool is running in a Docker container (i.e., without awareness of host's GOPATH). Go import paths in source units produced by the scanner may be inaccurate. To fix this, ensure that the --repo URI is specified. Report this issue if you are seeing it unexpectedly.")
	}

	units, err := golang.Scan("./...")
	if err != nil {
		return err
	}

	// fix up import paths to be consistent when running as a program and as
	// a Docker container.
	if os.Getenv("IN_DOCKER_CONTAINER") != "" {
		for _, u := range units {
			pkg := u.Data.(*build.Package)
			pkg.ImportPath = filepath.Join(c.Repo, c.Subdir, pkg.Dir)
			u.Name = pkg.ImportPath
		}
	}

	// make files relative to repository root
	for _, u := range units {
		pkgSubdir := filepath.Join(c.Subdir, u.Data.(*build.Package).Dir)
		for i, f := range u.Files {
			u.Files[i] = filepath.Join(pkgSubdir, f)
		}
	}

	// apply GoBaseImportPath config
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return err
	}
	for dir, ipp := range cfg.GoBaseImportPath {
		for _, u := range units {
			pkg := u.Data.(*build.Package)
			// rewrite all import paths using the new base
			if pathHasPrefix(pkg.Dir, dir) {
				importPathSubdirRelToDir, err := filepath.Rel(dir, pkg.Dir)
				if err != nil {
					return err
				}
				newImportPath := filepath.Join(ipp, importPathSubdirRelToDir)
				log.Printf("GoBaseImportPath: mapping package in dir %q with import path %q to new import path %q", pkg.Dir, pkg.ImportPath, newImportPath)
				u.Name = newImportPath
				pkg.ImportPath = newImportPath
			}
		}
	}

	if err := json.NewEncoder(os.Stdout).Encode(units); err != nil {
		return err
	}
	return nil
}
