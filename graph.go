package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/gddo/gosrc"

	"golang.org/x/tools/go/loader"

	"sourcegraph.com/sourcegraph/srclib-go/gog"
	"sourcegraph.com/sourcegraph/srclib-go/gog/definfo"
	defpkg "sourcegraph.com/sourcegraph/srclib-go/golang_def"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/grapher"
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

	// Check that we have the '-i' flag.
	cmd := exec.Command("go", "help", "build")
	o, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	usage := strings.Split(string(o), "\n")[0] // The usage is on the first line.
	matched, err := regexp.MatchString("-i", usage)
	if err != nil {
		log.Fatal(err)
	}
	if !matched {
		log.Fatal("'go build' does not have the '-i' flag. Please upgrade to go1.3+.")
	}
}

type GraphCmd struct{}

var graphCmd GraphCmd

// allowErrorsInGoGet is whether the grapher should continue after
// if `go get` fails.
var allowErrorsInGoGet = true

func (c *GraphCmd) Execute(args []string) error {
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

	if os.Getenv("IN_DOCKER_CONTAINER") != "" {
		buildPkg, err := UnitDataAsBuildPackage(unit)
		if err != nil {
			return err
		}

		// Make a new primary GOPATH.
		mainGOPATHDir := "/tmp/gopath"
		if buildContext.GOPATH == "" {
			buildContext.GOPATH = mainGOPATHDir
		} else {
			buildContext.GOPATH = mainGOPATHDir + ":" + buildContext.GOPATH
		}

		// Set up GOPATH so it has this repo.
		log.Printf("Setting up a new GOPATH at %s", mainGOPATHDir)
		dir := filepath.Join(mainGOPATHDir, "src", string(unit.Repo))
		if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
			return err
		}
		log.Printf("Creating symlink to oldname %q at newname %q.", cwd, dir)
		if err := os.Symlink(cwd, dir); err != nil {
			return err
		}

		// For every GOPATH that was in the Srcfile (or autodetected),
		// move it to a writable dir. (/src is not writable.)
		if config.GOPATH != "" {
			dirs := strings.Split(buildContext.GOPATH, ":")
			for i, dir := range dirs {
				if dir == mainGOPATHDir || dir == os.Getenv("GOPATH") {
					continue
				}

				oldSrcDir := filepath.Join(dir, "src")
				newGOPATH := filepath.Join("/tmp/gopath-" + strconv.Itoa(i) + "-" + filepath.Base(dir))
				newSrcDir := filepath.Join(newGOPATH, "src")
				log.Printf("Creating symlink for non-primary GOPATH to oldname %q at newname %q.", oldSrcDir, newSrcDir)
				if err := os.MkdirAll(filepath.Dir(newSrcDir), 0700); err != nil {
					return err
				}
				if err := os.Symlink(oldSrcDir, newSrcDir); err != nil {
					return err
				}
				dirs[i] = newGOPATH
			}
			buildContext.GOPATH = strings.Join(dirs, ":")
		}

		log.Printf("Changing directory to %q.", dir)
		if err := os.Chdir(dir); err != nil {
			return err
		}
		dockerCWD = cwd

		if config.GOROOT == "" {
			cwd = dir
		}

		// Get and install deps. But don't get deps that:
		//
		// * are in this repo; if we call `go get` on this repo, we will either try to check out a different
		//   version or fail with 'stale checkout?' because the .dockerignore doesn't copy the .git dir.
		// * are "C" as in `import "C"` for cgo
		// * have no slashes in their import path (which occurs when graphing the Go stdlib, for pkgs like "fmt");
		//   we'll just assume that these packages are never remote and do not need `go get`ting.
		var externalDeps []string
		for _, dep := range unit.Dependencies {
			importPath := dep.(string)
			if !strings.HasPrefix(importPath, string(unit.Repo)) && importPath != "C" && strings.Count(filepath.Clean(importPath), "/") > 0 {
				externalDeps = append(externalDeps, importPath)
			}
		}
		deps := append([]string{"./" + buildPkg.Dir}, externalDeps...)
		for _, dep := range deps {
			cmd := exec.Command("go", "get", "-d", "-t", "-v", dep)
			cmd.Args = append(cmd.Args)
			cmd.Env = config.env()
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			log.Printf("%v (env vars: %v).", cmd.Args, cmd.Env)
			if err := cmd.Run(); err != nil {
				if allowErrorsInGoGet {
					log.Printf("%v failed: %s (continuing)", cmd.Args, err)
				} else {
					return err
				}
			}
		}
		log.Printf("Finished downloading dependencies.")
	}

	out, err := Graph(unit)
	if err != nil {
		return err
	}

	// Make paths relative to repo.
	for _, gs := range out.Defs {
		if gs.File == "" {
			log.Printf("no file %+v", gs)
		}
		if gs.File != "" {
			gs.File = relPath(cwd, gs.File)
		}
	}
	for _, gr := range out.Refs {
		if gr.File != "" {
			gr.File = relPath(cwd, gr.File)
		}
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

func relPath(base, path string) string {
	rp, err := filepath.Rel(base, path)
	if err != nil {
		log.Fatalf("Failed to make path %q relative to %q: %s", path, base, err)
	}

	// TODO(sqs): hack
	if strings.HasPrefix(rp, "../../../") && dockerCWD != "" {
		rp, err = filepath.Rel(dockerCWD, path)
		if err != nil {
			log.Fatalf("Failed to make path %q relative to %q: %s", path, cwd, err)
		}
	}

	// TODO(sqs): hack
	if prefix := "../tmp/gopath-"; strings.HasPrefix(rp, prefix) {
		rp = rp[len(prefix)+2:]
	}

	return rp
}

func Graph(unit *unit.SourceUnit) (*grapher.Output, error) {
	pkg, err := UnitDataAsBuildPackage(unit)
	if err != nil {
		return nil, err
	}

	o, err := doGraph(pkg)
	if err != nil {
		return nil, err
	}

	o2 := grapher.Output{}

	uri := string(unit.Repo)

	for _, gs := range o.Defs {
		d, err := convertGoDef(gs, uri)
		if err != nil {
			return nil, err
		}
		if d != nil {
			o2.Defs = append(o2.Defs, d)
		}
	}
	for _, gr := range o.Refs {
		r, err := convertGoRef(gr, uri)
		if err != nil {
			return nil, err
		}
		if r != nil {
			o2.Refs = append(o2.Refs, r)
		}
	}
	for _, gd := range o.Docs {
		d, err := convertGoDoc(gd, uri)
		if err != nil {
			return nil, err
		}
		if d != nil {
			o2.Docs = append(o2.Docs, d)
		}
	}

	return &o2, nil
}

func convertGoDef(gs *gog.Def, repoURI string) (*graph.Def, error) {
	resolvedTarget, err := ResolveDep(gs.DefKey.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	path := graph.DefPath(pathOrDot(strings.Join(gs.Path, "/")))
	treePath := treePath(strings.Replace(string(path), ".go", "", -1))
	if !treePath.IsValid() {
		return nil, fmt.Errorf("'%s' is not a valid tree-path", treePath)
	}

	def := &graph.Def{
		DefKey: graph.DefKey{
			Unit:     resolvedTarget.ToUnit,
			UnitType: resolvedTarget.ToUnitType,
			Path:     path,
		},
		TreePath: treePath,

		Name: gs.Name,
		Kind: definfo.GeneralKindMap[gs.Kind],

		File:     gs.File,
		DefStart: gs.DeclSpan[0],
		DefEnd:   gs.DeclSpan[1],

		Exported: gs.DefInfo.Exported,
		Local:    !gs.DefInfo.Exported && !gs.DefInfo.PkgScope,
		Test:     strings.HasSuffix(gs.File, "_test.go"),
	}

	d := defpkg.DefData{
		PackageImportPath: gs.DefKey.PackageImportPath,
		DefInfo:           gs.DefInfo,
	}
	def.Data, err = json.Marshal(d)
	if err != nil {
		return nil, err
	}

	if def.File == "" {
		// some cgo defs have empty File; omit them
		return nil, nil
	}

	return def, nil
}

func convertGoRef(gr *gog.Ref, repoURI string) (*graph.Ref, error) {
	resolvedTarget, err := ResolveDep(gr.Def.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	if resolvedTarget == nil {
		return nil, nil
	}

	return &graph.Ref{
		DefRepo:     uriOrEmpty(resolvedTarget.ToRepoCloneURL),
		DefPath:     graph.DefPath(pathOrDot(strings.Join(gr.Def.Path, "/"))),
		DefUnit:     resolvedTarget.ToUnit,
		DefUnitType: resolvedTarget.ToUnitType,
		Def:         gr.IsDef,
		File:        gr.File,
		Start:       gr.Span[0],
		End:         gr.Span[1],
	}, nil
}

func convertGoDoc(gd *gog.Doc, repoURI string) (*graph.Doc, error) {
	resolvedTarget, err := ResolveDep(gd.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	return &graph.Doc{
		DefKey: graph.DefKey{
			Path:     graph.DefPath(pathOrDot(strings.Join(gd.Path, "/"))),
			Unit:     resolvedTarget.ToUnit,
			UnitType: resolvedTarget.ToUnitType,
		},
		Format: gd.Format,
		Data:   gd.Data,
		File:   gd.File,
		Start:  gd.Span[0],
		End:    gd.Span[1],
	}, nil
}

func uriOrEmpty(cloneURL string) string {
	if cloneURL == "" {
		return ""
	}
	return graph.MakeURI(cloneURL)
}

func pathOrDot(path string) string {
	if path == "" {
		return "."
	}
	return path
}

func treePath(path string) graph.TreePath {
	if path == "" || path == "." {
		return graph.TreePath(".")
	}
	return graph.TreePath(fmt.Sprintf("./%s", path))
}

// allowErrorsInGraph is whether the grapher should continue after
// encountering "reasonably common" errors (such as compile errors).
var allowErrorsInGraph = true

func doGraph(pkg *build.Package) (*gog.Output, error) {
	importPath := pkg.ImportPath

	if !loaderConfig.SourceImports {
		imports := map[string]struct{}{}
		for _, imp := range pkg.Imports {
			imports[imp] = struct{}{}
		}
		for _, imp := range pkg.TestImports {
			imports[imp] = struct{}{}
		}
		for _, imp := range pkg.XTestImports {
			imports[imp] = struct{}{}
		}

		for imp, _ := range imports {
			if imp == "C" {
				continue
			}
			if gosrc.IsGoRepoPath(imp) {
				// Optimization: don't bother installing builtin
				// packages because they're already installed. (But if
				// we accidentally install one, it's OK and not going
				// to cause any problems.)
				continue
			}
			cmd := exec.Command("go", "install", "-v", imp)
			cmd.Env = config.env()
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			log.Printf("Install %q: %v (env vars: %v)", importPath, cmd.Args, cmd.Env)
			if err := cmd.Run(); err != nil {
				if allowErrorsInGraph {
					log.Printf("Warning: failed to install package %q (command %v, env vars %v): %s. Continuing...", imp, cmd.Args, cmd.Env)
				} else {
					return nil, err
				}
			}
		}
	}

	importUnsafe := importPath == "unsafe"

	// Special-case: if this is a Cgo package, treat the CgoFiles as GoFiles or
	// else the character offsets will be junk.
	//
	// See https://codereview.appspot.com/86140043.
	loaderConfig.Build.CgoEnabled = false
	build.Default = *loaderConfig.Build
	if len(pkg.CgoFiles) > 0 {
		var allGoFiles []string
		allGoFiles = append(allGoFiles, pkg.GoFiles...)
		allGoFiles = append(allGoFiles, pkg.CgoFiles...)
		allGoFiles = append(allGoFiles, pkg.TestGoFiles...)
		for i, f := range allGoFiles {
			allGoFiles[i] = filepath.Join(cwd, pkg.Dir, f)
		}
		if err := loaderConfig.CreateFromFilenames(pkg.ImportPath, allGoFiles...); err != nil {
			return nil, err
		}
	} else {
		// Normal import
		if err := loaderConfig.ImportWithTests(importPath); err != nil {
			return nil, err
		}
	}

	if importUnsafe {
		// Special-case "unsafe" because go/loader does not let you load it
		// directly.
		if loaderConfig.ImportPkgs == nil {
			loaderConfig.ImportPkgs = make(map[string]bool)
		}
		loaderConfig.ImportPkgs["unsafe"] = true
	}

	prog, err := loaderConfig.Load()
	if err != nil {
		return nil, err
	}

	g := gog.New(prog)

	var pkgs []*loader.PackageInfo
	for _, pkg := range prog.Created {
		if strings.HasSuffix(pkg.Pkg.Name(), "_test") {
			// ignore xtest packages
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	for _, pkg := range prog.Imported {
		pkgs = append(pkgs, pkg)
	}

	for _, pkg := range pkgs {
		if err := g.Graph(pkg); err != nil {
			return nil, err
		}
	}

	return &g.Output, nil
}
