package main

import (
	"encoding/json"
	"go/build"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
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

	cwd, err := filepath.EvalSymlinks(getCWD())
	if err != nil {
		return err
	}
	scanDir := cwd
	if !isInGopath(scanDir) {
		scanDir = filepath.Join(cwd, srclibGopath, "src", filepath.FromSlash(config.ImportPathRoot), filepath.FromSlash(c.Repo))
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

	units, err := scan(scanDir)
	if err != nil {
		return err
	}

	if len(config.PkgPatterns) != 0 {
		matchers := make([]func(name string) bool, len(config.PkgPatterns))
		for i, pattern := range config.PkgPatterns {
			matchers[i] = matchPattern(pattern)
		}

		var filteredUnits []*unit.SourceUnit
		for _, unit := range units {
			for _, m := range matchers {
				if m(unit.Name) {
					filteredUnits = append(filteredUnits, unit)
					break
				}
			}
		}
		units = filteredUnits
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

func scan(scanDir string) ([]*unit.SourceUnit, error) {
	// TODO(sqs): include xtest, but we'll have to make them have a distinctly
	// namespaced def path from the non-xtest pkg.

	pkgs, err := scanForPackages(scanDir)
	if err != nil {
		return nil, err
	}

	var units []*unit.SourceUnit
	for _, pkg := range pkgs {
		// Collect all files
		var files []string
		files = append(files, pkg.GoFiles...)
		files = append(files, pkg.CgoFiles...)
		files = append(files, pkg.IgnoredGoFiles...)
		files = append(files, pkg.CFiles...)
		files = append(files, pkg.CXXFiles...)
		files = append(files, pkg.MFiles...)
		files = append(files, pkg.HFiles...)
		files = append(files, pkg.SFiles...)
		files = append(files, pkg.SwigFiles...)
		files = append(files, pkg.SwigCXXFiles...)
		files = append(files, pkg.SysoFiles...)
		files = append(files, pkg.TestGoFiles...)
		files = append(files, pkg.XTestGoFiles...)

		// Collect all imports. We use a map to remove duplicates.
		var imports []string
		imports = append(imports, pkg.Imports...)
		imports = append(imports, pkg.TestImports...)
		imports = append(imports, pkg.XTestImports...)
		imports = uniq(imports)
		sort.Strings(imports)

		// Create appropriate type for (unit).SourceUnit
		deps := make([]interface{}, len(imports))
		for i, imp := range imports {
			deps[i] = imp
		}

		pkg.Dir, err = filepath.Rel(scanDir, pkg.Dir)
		if err != nil {
			return nil, err
		}
		pkg.BinDir = ""
		pkg.ConflictDir = ""

		// Root differs depending on the system, so it's hard to compare results
		// across environments (when running as a program). Clear it so we can
		// compare results in tests more easily.
		pkg.Root = ""
		pkg.SrcRoot = ""
		pkg.PkgRoot = ""

		pkg.ImportPos = nil
		pkg.TestImportPos = nil
		pkg.XTestImportPos = nil

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

	return units, nil
}

func scanForPackages(dir string) ([]*build.Package, error) {
	var pkgs []*build.Package

	pkg, err := buildContext.ImportDir(dir, 0)
	if _, isNoGoError := err.(*build.NoGoError); err != nil && !isNoGoError {
		return nil, err
	}
	if err == nil {
		pkgs = append(pkgs, pkg)
	}

	infos, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		name := info.Name()
		fullPath := filepath.Join(dir, name)
		if info.IsDir() && ((name[0] != '.' && name[0] != '_' && name != "testdata") || (strings.HasSuffix(filepath.ToSlash(fullPath), "/Godeps/_workspace") && !config.SkipGodeps)) {
			subPkgs, err := scanForPackages(fullPath)
			if err != nil {
				return nil, err
			}
			pkgs = append(pkgs, subPkgs...)
		}
	}

	return pkgs, nil
}

// matchPattern(pattern)(name) reports whether
// name matches pattern.  Pattern is a limited glob
// pattern in which '...' means 'any string' and there
// is no other special syntax.
func matchPattern(pattern string) func(name string) bool {
	re := regexp.QuoteMeta(pattern)
	re = strings.Replace(re, `\.\.\.`, `.*`, -1)
	// Special case: foo/... matches foo too.
	if strings.HasSuffix(re, `/.*`) {
		re = re[:len(re)-len(`/.*`)] + `(/.*)?`
	}
	reg := regexp.MustCompile(`^` + re + `$`)
	return func(name string) bool {
		return reg.MatchString(name)
	}
}
