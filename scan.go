package main

import (
	"encoding/json"
	"go/build"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"sort"
	"strings"

	"sourcegraph.com/sourcegraph/srclib/toolchain"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

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

	if err := json.NewDecoder(os.Stdin).Decode(&config); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}
	if err := config.apply(); err != nil {
		return err
	}

	units, err := scan("./...")
	if err != nil {
		return err
	}

	// Fix up import paths to be consistent when running as a program and as
	// a Docker container. But if a GOROOT is set, then we probably want import
	// paths to not contain the repo, so only do this if there's no GOROOT set
	// in the Srcfile.
	if os.Getenv("IN_DOCKER_CONTAINER") != "" && config.GOROOT == "" {
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

	if err := json.NewEncoder(os.Stdout).Encode(units); err != nil {
		return err
	}
	return nil
}

func scan(pkgPattern string) ([]*unit.SourceUnit, error) {
	// TODO(sqs): include xtest, but we'll have to make them have a distinctly
	// namespaced def path from the non-xtest pkg.

	cmd := exec.Command("go", "list", "-e", "-json", pkgPattern)
	cmd.Env = config.env()
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	dec := json.NewDecoder(stdout)
	var units []*unit.SourceUnit
	for {
		var pkg *build.Package
		if err := dec.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		pv, pt := reflect.ValueOf(pkg).Elem(), reflect.TypeOf(*pkg)

		// collect all files
		var files []string
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if strings.HasSuffix(f.Name, "Files") {
				fv := pv.Field(i).Interface()
				files = append(files, fv.([]string)...)
			}
		}

		// collect all imports
		depsMap := map[string]struct{}{}
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if strings.HasSuffix(f.Name, "Imports") {
				fv := pv.Field(i).Interface()
				imports := fv.([]string)
				for _, imp := range imports {
					depsMap[imp] = struct{}{}
				}
			}
		}
		deps0 := make([]string, len(depsMap))
		i := 0
		for imp := range depsMap {
			deps0[i] = imp
			i++
		}
		sort.Strings(deps0)
		deps := make([]interface{}, len(deps0))
		for i, imp := range deps0 {
			deps[i] = imp
		}

		// make all dirs relative to the current dir
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if strings.HasSuffix(f.Name, "Dir") {
				fv := pv.Field(i)
				dir := fv.Interface().(string)
				if dir != "" {
					dir, err := filepath.Rel(cwd, dir)
					if err != nil {
						return nil, err
					}
					fv.Set(reflect.ValueOf(dir))
				}
			}
		}

		// Root differs depending on the system, so it's hard to compare results
		// across environments (when running as a program). Clear it so we can
		// compare results in tests more easily.
		pkg.Root = ""

		units = append(units, &unit.SourceUnit{
			Name:         pkg.ImportPath,
			Type:         "GoPackage",
			Dir:          pkg.Dir,
			Files:        files,
			Data:         pkg,
			Dependencies: deps,
			Ops:          map[string]*toolchain.ToolRef{"depresolve": nil, "graph": nil},
		})
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return units, nil
}
