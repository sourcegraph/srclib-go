package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"sort"
	"strings"

	"sourcegraph.com/sourcegraph/srclib"
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

	// Automatically detect vendored dirs (check for vendor/src and
	// Godeps/_workspace/src) and set up GOPATH pointing to them if
	// they exist.
	var setAutoGOPATH bool
	if config.GOPATH == "" {
		vendorDirs := []string{"vendor", "Godeps/_workspace"}
		var foundGOPATHs []string
		for _, vdir := range vendorDirs {
			if fi, err := os.Stat(filepath.Join(cwd, vdir, "src")); err == nil && fi.Mode().IsDir() {
				foundGOPATHs = append(foundGOPATHs, vdir)
				setAutoGOPATH = true
				log.Printf("Adding %s to GOPATH (auto-detected Go vendored dependencies source dir %s). If you don't want this, make a Srcfile with a GOPATH property set to something other than the empty string.", vdir, filepath.Join(vdir, "src"))
			}
		}
		config.GOPATH = strings.Join(foundGOPATHs, string(filepath.ListSeparator))
	}

	if err := config.apply(); err != nil {
		return err
	}

	cwd := getCWD()
	scanDir := cwd
	if !isInGopath(scanDir) {
		scanDir = filepath.Join(cwd, srclibGopath, "src", filepath.FromSlash(c.Repo))
		buildContext.GOPATH = filepath.Join(cwd, srclibGopath) + string(os.PathListSeparator) + buildContext.GOPATH

		os.RemoveAll(srclibGopath) // ignore error
		if err := os.MkdirAll(filepath.Dir(scanDir), 0777); err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(scanDir), cwd)
		if err != nil {
			return err
		}
		if err := os.Symlink(rel, scanDir); err != nil {
			return err
		}
	}

	var pkgPatterns []string
	if config.PkgPatterns != nil {
		pkgPatterns = config.PkgPatterns
	} else {
		pkgPatterns = []string{"./..."}
	}
	units, err := scan(pkgPatterns, scanDir)
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

	// Make vendored dep unit names (package import paths) relative to
	// vendored src dir, not to top-level dir.
	if config.GOPATH != "" {
		dirs := filepath.SplitList(config.GOPATH)
		for _, dir := range dirs {
			relDir, err := filepath.Rel(cwd, dir)
			if err != nil {
				return err
			}
			srcDir := filepath.Join(relDir, "src")
			for _, u := range units {
				pkg := u.Data.(*build.Package)
				if strings.HasPrefix(pkg.Dir, srcDir) {
					relImport, err := filepath.Rel(srcDir, pkg.Dir)
					if err != nil {
						return err
					}
					pkg.ImportPath = relImport
					u.Name = pkg.ImportPath
				}
			}
		}
	}

	// make files relative to repository root
	for _, u := range units {
		pkgSubdir := filepath.Join(c.Subdir, u.Data.(*build.Package).Dir)
		for i, f := range u.Files {
			u.Files[i] = filepath.Join(pkgSubdir, f)
		}
	}

	// If we automatically set the GOPATH based on the presence of
	// vendor dirs, then we need to pass the GOPATH to the units
	// because it is not persisted in the Srcfile. Otherwise the other
	// tools would never see the auto-set GOPATH.
	if setAutoGOPATH {
		for _, u := range units {
			if u.Config == nil {
				u.Config = map[string]interface{}{}
			}

			dirs := filepath.SplitList(config.GOPATH)
			for i, dir := range dirs {
				relDir, err := filepath.Rel(cwd, dir)
				if err != nil {
					return err
				}
				dirs[i] = relDir
			}
			u.Config["GOPATH"] = strings.Join(dirs, string(filepath.ListSeparator))
		}
	}

	b, err := json.MarshalIndent(units, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(b); err != nil {
		return err
	}
	return nil
}

func isInGopath(path string) bool {
	for _, gopath := range filepath.SplitList(buildContext.GOPATH) {
		if strings.HasPrefix(evalSymlinks(path), filepath.Join(evalSymlinks(gopath), "src")) {
			return true
		}
	}
	return false
}

func scan(pkgPatterns []string, scanDir string) ([]*unit.SourceUnit, error) {
	// TODO(sqs): include xtest, but we'll have to make them have a distinctly
	// namespaced def path from the non-xtest pkg.

	// Go always evaluates symlinks in the working directory path, using sh is a workaround
	cmd := exec.Command("sh", "-c", fmt.Sprintf(`cd %s; %s list -e -json %s`, scanDir, goBinaryName, strings.Join(pkgPatterns, " ")))
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
					dir, err := filepath.Rel(scanDir, dir)
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
			Ops:          map[string]*srclib.ToolRef{"depresolve": nil, "graph": nil},
		})
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return units, nil
}
