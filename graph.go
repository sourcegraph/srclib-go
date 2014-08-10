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
	"strings"

	"code.google.com/p/go.tools/go/loader"
	"code.google.com/p/go.tools/godoc/vfs"

	"sourcegraph.com/sourcegraph/srclib-go/gog"
	"sourcegraph.com/sourcegraph/srclib-go/gog/definfo"
	defpkg "sourcegraph.com/sourcegraph/srclib-go/golang_def"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/grapher"
	"sourcegraph.com/sourcegraph/srclib/repo"
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

		// Make a new GOPATH.
		buildContext.GOPATH = "/tmp/gopath"

		// Set up GOPATH so it has this repo.
		log.Printf("Setting up a new GOPATH at %s", buildContext.GOPATH)
		dir := filepath.Join(buildContext.GOPATH, "src", string(unit.Repo))
		if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
			return err
		}
		log.Printf("Creating symlink to oldname %q at newname %q.", cwd, dir)
		if err := os.Symlink(cwd, dir); err != nil {
			return err
		}

		log.Printf("Changing directory to %q.", dir)
		if err := os.Chdir(dir); err != nil {
			return err
		}
		dockerCWD = cwd

		if config.GOROOT == "" {
			cwd = dir
		}

		// Get and install deps. (Only deps not in this repo; if we call `go
		// get` on this repo, we will either try to check out a different
		// version or fail with 'stale checkout?' because the .dockerignore
		// doesn't copy the .git dir.)
		var externalDeps []string
		for _, dep := range unit.Dependencies {
			importPath := dep.(string)
			if !strings.HasPrefix(importPath, string(unit.Repo)) && importPath != "C" {
				externalDeps = append(externalDeps, importPath)
			}
		}
		cmd := exec.Command("go", "get", "-x", "-d", "-t", "-v", "./"+buildPkg.Dir)
		cmd.Args = append(cmd.Args, externalDeps...)
		cmd.Env = config.env()
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		log.Printf("Downloading import dependencies: %v (env vars: %v).", cmd.Args, cmd.Env)
		if err := cmd.Run(); err != nil {
			return err
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

	return rp
}

func Graph(unit *unit.SourceUnit) (*grapher.Output, error) {
	pkg, err := UnitDataAsBuildPackage(unit)
	if err != nil {
		return nil, err
	}

	o, err := doGraph(pkg.ImportPath)
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
	treePath := treePath(string(path))
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
		Kind: graph.DefKind(definfo.GeneralKindMap[gs.Kind]),

		File:     gs.File,
		DefStart: gs.DeclSpan[0],
		DefEnd:   gs.DeclSpan[1],

		Exported: gs.DefInfo.Exported,
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

	if def.Kind == "func" {
		def.Callable = true
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

func uriOrEmpty(cloneURL string) repo.URI {
	if cloneURL == "" {
		return ""
	}
	return repo.MakeURI(cloneURL)
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

func doGraph(importPath string) (*gog.Output, error) {
	// If we've overridden GOROOT and we're building a package not in
	// $GOROOT/src/pkg (such as "cmd/go"), then we need to virtualize GOROOT
	// because we can't set GOPATH=GOROOT (go/build ignores GOPATH in that
	// case).
	if config.GOROOT != "" && strings.HasPrefix(importPath, "cmd/") {
		// Unset our custom GOROOT (since we're routing FS ops to it using
		// vfs) and set it as our GOPATH.
		buildContext.GOROOT = build.Default.GOROOT
		buildContext.GOPATH = config.GOROOT

		virtualCWD = build.Default.GOROOT

		ns := vfs.NameSpace{}
		ns.Bind(filepath.Join(buildContext.GOROOT, "src/pkg"), vfs.OS(filepath.Join(config.GOROOT, "src/pkg")), "/", vfs.BindBefore)
		ns.Bind("/", vfs.OS("/"), "/", vfs.BindAfter)
		buildContext.IsDir = func(path string) bool {
			fi, err := ns.Stat(path)
			return err == nil && fi.Mode().IsDir()
		}
		buildContext.HasSubdir = func(root, dir string) (rel string, ok bool) { panic("unexpected") }
		buildContext.OpenFile = func(path string) (io.ReadCloser, error) {
			f, err := ns.Open(path)
			return f, err
		}
		buildContext.ReadDir = ns.ReadDir
	}

	// If we're using a custom GOROOT, we need to bootstrap the installation.
	if config.GOROOT != "" && !loaderConfig.SourceImports {
		if _, err := os.Stat("src/make.bash"); err == nil {
			// TODO(sqs): in docker, we can't write to this dir, so we'll have to
			// move it elsewhere
			cmd := exec.Command("bash", "make.bash")
			cmd.Dir = filepath.Join(cwd, "src")
			cmd.Env = config.env()
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			log.Printf("Bootstrapping: %v (env vars: %v)", cmd.Args, cmd.Env)
			if err := cmd.Run(); err != nil {
				return nil, err
			}
		}
	}

	if !loaderConfig.SourceImports {
		// Install pkg.
		cmd := exec.Command("go", "install", "-x", "-v", importPath)
		cmd.Env = config.env()
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		log.Printf("Install %q: %v (env vars: %v)", importPath, cmd.Args, cmd.Env)
		if err := cmd.Run(); err != nil {
			return nil, err
		}

		log.Println("\n\n############### AFTER BUILD ###############\n\n")
		out, err := exec.Command("find", "/tmp/gopath").Output()
		log.Println(string(out))
		log.Println(err)
	}

	importUnsafe := importPath == "unsafe"

	if err := loaderConfig.ImportWithTests(importPath); err != nil {
		return nil, err
	}

	if importUnsafe {
		// Special-case "unsafe" because go/loader does not let you load it
		// directly.
		if loaderConfig.ImportPkgs == nil {
			loaderConfig.ImportPkgs = make(map[string]bool)
		}
		loaderConfig.ImportPkgs["unsafe"] = true
	}

	build.Default = *loaderConfig.Build

	prog, err := loaderConfig.Load()
	if err != nil {
		return nil, err
	}

	g := gog.New(prog)

	var pkgs []*loader.PackageInfo
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
