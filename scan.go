package main

import (
	"encoding/json"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/gcimporter15"

	"sourcegraph.com/sourcegraph/srclib/unit"
)

func init() {
	_, err := flagParser.AddCommand("scan",
		"scan for Go packages",
		"Scan the directory tree rooted at the current directory for Go packages.",
		&scanCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

type ScanCmd struct{}

var scanCmd ScanCmd

func (c *ScanCmd) Execute(args []string) error {
	if err := initBuildContext(); err != nil {
		return err
	}

	scanDir, err := filepath.EvalSymlinks(getCWD())
	if err != nil {
		return err
	}

	units, err := scan(scanDir)
	if err != nil {
		return err
	}

	// Make go1.5 style vendored dep unit names (package import paths)
	// relative to vendored dir, not to top-level dir.
	for _, u := range units {
		var pkg build.Package
		if err := json.Unmarshal(u.Data, &pkg); err != nil {
			return err
		}
		if name, isVendored := vendoredUnitName(&pkg); isVendored {
			u.Name = name
		}
	}

	// make files relative to repository root
	for _, u := range units {
		var pkg build.Package
		if err := json.Unmarshal(u.Data, &pkg); err != nil {
			return err
		}
		pkgSubdir := pkg.Dir
		for i, f := range u.Files {
			u.Files[i] = filepath.ToSlash(filepath.Join(pkgSubdir, f))
		}
	}

	// Find vendored units to build a list of vendor directories
	vendorDirs := map[string]struct{}{}
	for _, u := range units {
		unixStyle := filepath.ToSlash(u.Dir)
		i, ok := findVendor(unixStyle)
		// Don't include old style vendor dirs
		if !ok || strings.HasPrefix(unixStyle[i:], "vendor/src/") {
			continue
		}
		vendorDirs[unixStyle[:i+len("vendor")]] = struct{}{}
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

// findVendor from golang/go/cmd/go/pkg.go
func findVendor(path string) (index int, ok bool) {
	// Two cases, depending on internal at start of string or not.
	// The order matters: we must return the index of the final element,
	// because the final one is where the effective import path starts.
	switch {
	case strings.Contains(path, "/vendor/"):
		return strings.LastIndex(path, "/vendor/") + 1, true
	case strings.HasPrefix(path, "vendor/"):
		return 0, true
	}
	return 0, false
}

// vendoredUnitName returns the proper unit name of a Go package if it
// is vendored. If the package is not vendored, it returns the empty
// string and false.
func vendoredUnitName(pkg *build.Package) (name string, isVendored bool) {
	unixStyle := filepath.ToSlash(pkg.Dir)
	i, ok := findVendor(unixStyle)
	if !ok {
		return "", false
	}
	relDir := unixStyle[i+len("vendor"):]
	if strings.HasPrefix(relDir, "/src/") || !strings.HasPrefix(relDir, "/") {
		return "", false
	}
	relImport := relDir[1:]
	return relImport, true
}

func scan(scanDir string) ([]*unit.SourceUnit, error) {
	// TODO(sqs): include xtest, but we'll have to make them have a distinctly
	// namespaced def path from the non-xtest pkg.

	filteredScanDir := scanDir
	if buildContext.GOROOT == cwd { // Go stdlib
		filteredScanDir = filepath.Join(scanDir, "src")
	}
	pkgs, err := scanForPackages(filteredScanDir)
	if err != nil {
		return nil, err
	}

	var units []*unit.SourceUnit
	for _, pkg := range pkgs {
		var allImports []string
		allImports = append(allImports, pkg.Imports...)
		allImports = append(allImports, pkg.TestImports...)
		allImports = append(allImports, pkg.XTestImports...)
		if _, err := prepareDependencies(allImports, pkg.ImportPath, pkg.Dir, token.NewFileSet()); err != nil {
			return nil, err
		}

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

		// Collect all imports. We use a map to remove duplicates.
		var imports []string
		imports = append(imports, pkg.Imports...)
		imports = append(imports, pkg.TestImports...)
		imports = append(imports, pkg.XTestImports...)
		imports = uniq(imports)
		sort.Strings(imports)

		// Create appropriate type for (unit).SourceUnit
		deps := make([]*unit.Key, len(imports))
		for i, imp := range imports {
			deps[i] = &unit.Key{
				Repo: unit.UnitRepoUnresolved,
				Type: "GoPackage",
				Name: imp,
			}
		}

		pkg.Dir, err = filepath.Rel(scanDir, pkg.Dir)
		if err != nil {
			return nil, err
		}
		pkg.Dir = filepath.ToSlash(pkg.Dir)
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

		pkgData, err := json.Marshal(pkg)
		if err != nil {
			return nil, err
		}

		units = append(units, &unit.SourceUnit{
			Key: unit.Key{
				Name: pkg.ImportPath,
				Type: "GoPackage",
			},
			Info: unit.Info{
				Dir:          pkg.Dir,
				Files:        files,
				Data:         pkgData,
				Dependencies: deps,
				Ops:          map[string][]byte{"depresolve": nil, "graph": nil},
			},
		})

		if len(pkg.XTestGoFiles) != 0 {
			units = append(units, &unit.SourceUnit{
				Key: unit.Key{
					Name: pkg.ImportPath + "_test",
					Type: "GoPackage",
				},
				Info: unit.Info{
					Dir:          pkg.Dir,
					Files:        pkg.XTestGoFiles,
					Data:         pkgData,
					Dependencies: deps,
					Ops:          map[string][]byte{"depresolve": nil, "graph": nil},
				},
			})
		}
	}

	return units, nil
}

func scanForPackages(dir string) ([]*build.Package, error) {
	var pkgs []*build.Package

	pkg, err := buildContext.ImportDir(dir, 0)
	if err != nil {
		if _, ok := err.(*build.NoGoError); ok {
			// ignore
		} else {
			log.Printf("Error scanning %s for packages: %v. Ignoring source files in this directory.", dir, err)
		}
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
		if info.IsDir() && ((name[0] != '.' && name[0] != '_' && name != "testdata") || (strings.HasSuffix(filepath.ToSlash(fullPath), "/Godeps/_workspace"))) {
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

// StringSlice attaches the methods of sort.Interface to []string, sorting in decreasing string length
type vendorDirSlice []string

func (p vendorDirSlice) Len() int           { return len(p) }
func (p vendorDirSlice) Less(i, j int) bool { return len(p[i]) >= len(p[j]) }
func (p vendorDirSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// prepareDependencies performs best-effort fetches and builds of the
// packages whose import paths are given by imports. If fetching a
// package fails, a warning is printed and the package is skipped.
func prepareDependencies(imports []string, currentPkg string, srcDir string, fset *token.FileSet) (map[string]*types.Package, error) {
	dependencies := map[string]*types.Package{
		"unsafe": types.Unsafe,
	}
	packages := map[string]*types.Package{}

	for _, path := range imports {
		if path == "unsafe" || path == "C" || path == currentPkg {
			continue
		}

		if build.IsLocalImport(path) {
			log.Printf("warning: local imports not supported: %s", path)
			continue
		}

		impPkg, err := buildContext.Import(path, srcDir, build.AllowBinary)
		if err != nil {
			// This step can fail when a dependency package has been
			// moved or its host is down. Failures here will degrade
			// the analysis, but they should not be fatal, or else the
			// build success is very sensitive to external
			// dependencies.

			// try to download package
			cmd := exec.Command("go", "get", "-d", "-v", path)
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GOROOT=" + buildContext.GOROOT, "GOPATH=" + buildContext.GOPATH}
			if err := cmd.Run(); err != nil {
				log.Printf("warning: fetching dependency (with %v) failed: %s", cmd.Args, err)
				continue
			}

			impPkg, err = buildContext.Import(path, srcDir, build.AllowBinary)
			if err != nil {
				log.Printf("warning: importing dependency %q failed: %s", path, err)
				continue
			}
		}

		typesPkg, ok := packages[impPkg.ImportPath]
		if !ok || !typesPkg.Complete() {
			if _, err := os.Stat(impPkg.PkgObj); os.IsNotExist(err) {
				if err := writePkgObj(impPkg); err != nil {
					return nil, err
				}
			}

			data, err := ioutil.ReadFile(impPkg.PkgObj)
			if err != nil {
				return nil, err
			}
			_, typesPkg, err = gcimporter.BImportData(fset, packages, data, impPkg.ImportPath)
			if err != nil {
				return nil, err
			}
		}

		dependencies[path] = typesPkg
	}

	return dependencies, nil
}

func writePkgObj(buildPkg *build.Package) error {
	fset := token.NewFileSet()
	dependencies, err := prepareDependencies(buildPkg.Imports, buildPkg.ImportPath, buildPkg.Dir, fset)
	if err != nil {
		return err
	}

	var files []*ast.File
	for _, name := range append(buildPkg.GoFiles, buildPkg.CgoFiles...) {
		file, err := parser.ParseFile(fset, filepath.Join(buildPkg.Dir, name), nil, parser.ParseComments)
		if err != nil {
			return err
		}
		files = append(files, file)
	}

	typesConfig := &types.Config{
		Importer:    mapImporter(dependencies),
		FakeImportC: true,
		Error: func(err error) {
			// errors are ignored, use best-effort type checking output
		},
	}
	typesPkg, err := typesConfig.Check(buildPkg.ImportPath, fset, files, nil)
	if err != nil {
		log.Println("type checker error:", err) // see comment above
	}

	if err := os.MkdirAll(filepath.Dir(buildPkg.PkgObj), 0777); err != nil {
		return err
	}
	return ioutil.WriteFile(buildPkg.PkgObj, gcimporter.BExportData(fset, typesPkg), 0666)
}
