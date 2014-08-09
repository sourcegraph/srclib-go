package main

import (
	"encoding/json"
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sourcegraph.com/sourcegraph/srclib-go/golang"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

func init() {
	_, err := parser.AddCommand("graph",
		"graph a Go package",
		"Graph a Go package, producing all defs, refs, and docs.",
		&graphCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

type GraphCmd struct {
	Config []string `long:"config" description:"config property from Srcfile" value-name:"KEY=VALUE"`
}

var graphCmd GraphCmd

func (c *GraphCmd) Execute(args []string) error {
	var unit *unit.SourceUnit
	if err := json.NewDecoder(os.Stdin).Decode(&unit); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}

	// TODO(sqs) TMP remove

	if os.Getenv("IN_DOCKER_CONTAINER") != "" {
		buildPkg, err := golang.UnitDataAsBuildPackage(unit)
		if err != nil {
			return err
		}

		// Make a new GOPATH.
		build.Default.GOPATH = "/tmp/gopath"

		// Set up GOPATH so it has this repo.
		dir := filepath.Join(build.Default.GOPATH, "src", string(unit.Repo))
		if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
			return err
		}
		if err := os.Symlink(cwd, dir); err != nil {
			return err
		}

		if err := os.Chdir(dir); err != nil {
			return err
		}
		cwd = dir

		if err := os.Setenv("GOPATH", build.Default.GOPATH); err != nil {
			return err
		}

		// Get and install deps. (Only deps not in this repo; if we call `go
		// get` on this repo, we will either try to check out a different
		// version or fail with 'stale checkout?' because the .dockerignore
		// doesn't copy the .git dir.)
		var externalDeps []string
		for _, dep := range unit.Dependencies {
			importPath := dep.(string)
			if !strings.HasPrefix(importPath, string(unit.Repo)) {
				externalDeps = append(externalDeps, importPath)
			}
		}
		cmd := exec.Command("go", "get", "-d", "-t", "-v", "./"+buildPkg.Dir)
		cmd.Args = append(cmd.Args, externalDeps...)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		log.Println(cmd.Args)
		if err := cmd.Run(); err != nil {
			return err
		}
		cmd = exec.Command("go", "build", "-i", "./"+buildPkg.Dir)
		cmd.Args = append(cmd.Args, externalDeps...)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		log.Println(cmd.Args)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	c.Config = []string{"GoBaseImportPath:src/pkg=."}
	cfg, err := parseConfig(c.Config)
	if err != nil {
		return err
	}
	_ = cfg

	out, err := golang.Graph(unit)
	if err != nil {
		return err
	}

	// Make paths relative to repo.
	for _, gs := range out.Defs {
		if gs.File == "" {
			log.Printf("no file %+v", gs)
		}
		gs.File = relPath(cwd, gs.File)
	}
	for _, gr := range out.Refs {
		gr.File = relPath(cwd, gr.File)
	}
	for _, gd := range out.Docs {
		if gd.File != "" {
			gd.File = relPath(cwd, gd.File)
		}
	}

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return err
	}
	return nil
}

func relPath(cwd, path string) string {
	rp, err := filepath.Rel(cwd, path)
	if err != nil {
		log.Fatalf("Failed to make path %q relative to %q: %s", path, cwd, err)
	}
	return rp
}
