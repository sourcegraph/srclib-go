package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jessevdk/go-flags"
	"github.com/sourcegraph/srclib-go/golang"
	"github.com/sourcegraph/srclib/dep"
	"github.com/sourcegraph/srclib/unit"
)

var (
	parser = flags.NewNamedParser("srclib-go", flags.Default)
	cwd    string
)

func init() {
	parser.LongDescription = "srclib-go performs Go package, dependency, and source analysis."

	var err error
	cwd, err = os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	log.SetFlags(0)
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}
}

func init() {
	_, err := parser.AddCommand("scan",
		"scan for Go packages",
		"Scan the directory tree rooted at the current directory for Go packages.",
		&scanCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

type ScanCmd struct {
	Repo   string `long:"repo" description:"repository URI" value-name:"URI"`
	Subdir string `long:"subdir" description:"subdirectory in repository" value-name:"DIR"`
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

	if err := json.NewEncoder(os.Stdout).Encode(units); err != nil {
		return err
	}
	return nil
}

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

type DepResolveCmd struct{}

var depResolveCmd DepResolveCmd

func (c *DepResolveCmd) Execute(args []string) error {
	var unit *unit.SourceUnit
	if err := json.NewDecoder(os.Stdin).Decode(&unit); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}

	res := make([]*dep.Resolution, len(unit.Dependencies))
	for i, rawDep := range unit.Dependencies {
		importPath, ok := rawDep.(string)
		if !ok {
			return fmt.Errorf("Go raw dep is not a string import path: %v (%T)", rawDep, rawDep)
		}

		res[i] = &dep.Resolution{Raw: rawDep}

		rt, err := golang.ResolveDep(importPath, string(unit.Repo))
		if err != nil {
			res[i].Error = err.Error()
			continue
		}
		res[i].Target = rt
	}

	if err := json.NewEncoder(os.Stdout).Encode(res); err != nil {
		return err
	}
	return nil
}

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

type GraphCmd struct{}

var graphCmd GraphCmd

func (c *GraphCmd) Execute(args []string) error {
	var unit *unit.SourceUnit
	if err := json.NewDecoder(os.Stdin).Decode(&unit); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}

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

	out, err := golang.Graph(unit)
	if err != nil {
		return err
	}

	// Make paths relative to repo.
	for _, gs := range out.Symbols {
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
