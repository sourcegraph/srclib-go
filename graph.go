package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
	"strings"

	"golang.org/x/tools/go/gcimporter15"

	"sourcegraph.com/sourcegraph/srclib-go/gog"
	"sourcegraph.com/sourcegraph/srclib-go/gog/definfo"
	defpkg "sourcegraph.com/sourcegraph/srclib-go/golang_def"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

func init() {
	_, err := flagParser.AddCommand("graph",
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
	inputBytes, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var unit *unit.SourceUnit
	if err := json.NewDecoder(bytes.NewReader(inputBytes)).Decode(&unit); err != nil {
		return err
	}
	if err := os.Stdin.Close(); err != nil {
		return err
	}

	if err := initBuildContext(); err != nil {
		return err
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
	rp, err := filepath.Rel(evalSymlinks(base), evalSymlinks(path))
	if err != nil {
		log.Fatalf("Failed to make path %q relative to %q: %s", path, base, err)
	}
	return filepath.ToSlash(rp)
}

func Graph(unit *unit.SourceUnit) (*graph.Output, error) {
	pkg, err := UnitDataAsBuildPackage(unit)
	if err != nil {
		return nil, err
	}

	o, err := doGraph(pkg)
	if err != nil {
		return nil, err
	}

	o2 := graph.Output{}

	for _, gs := range o.Defs {
		d, err := convertGoDef(gs)
		if err != nil {
			log.Printf("Ignoring def %v due to error in converting to GoDef: %s.", gs, err)
			continue
		}
		if d != nil {
			o2.Defs = append(o2.Defs, d)
		}
	}
	for _, gr := range o.Refs {
		r, err := convertGoRef(gr)
		if err != nil {
			log.Printf("Ignoring ref %v due to error in converting to GoRef: %s.", gr, err)
			continue
		}
		if r != nil {
			o2.Refs = append(o2.Refs, r)
		}
	}
	for _, gd := range o.Docs {
		d, err := convertGoDoc(gd)
		if err != nil {
			log.Printf("Ignoring doc %v due to error in converting to GoDoc: %s.", gd, err)
			continue
		}
		if d != nil {
			o2.Docs = append(o2.Docs, d)
		}
	}

	return &o2, nil
}

func convertGoDef(gs *gog.Def) (*graph.Def, error) {
	resolvedTarget, err := ResolveDep(gs.DefKey.PackageImportPath)
	if err != nil {
		return nil, err
	}
	path := filepath.ToSlash(pathOrDot(filepath.Join(gs.Path...)))
	treePath := treePath(strings.Replace(string(path), ".go", "", -1))
	if !graph.IsValidTreePath(treePath) {
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

		File:     filepath.ToSlash(gs.File),
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

func convertGoRef(gr *gog.Ref) (*graph.Ref, error) {
	resolvedTarget, err := ResolveDep(gr.Def.PackageImportPath)
	if err != nil {
		return nil, err
	}
	if resolvedTarget == nil {
		return nil, nil
	}

	resolvedRefUnit, err := ResolveDep(gr.Unit)
	if err != nil {
		return nil, err
	}
	if resolvedRefUnit == nil {
		return nil, nil
	}

	return &graph.Ref{
		DefRepo:     filepath.ToSlash(uriOrEmpty(resolvedTarget.ToRepoCloneURL)),
		DefPath:     filepath.ToSlash(pathOrDot(filepath.Join(gr.Def.Path...))),
		DefUnit:     resolvedTarget.ToUnit,
		DefUnitType: resolvedTarget.ToUnitType,
		Def:         gr.IsDef,
		Unit:        resolvedRefUnit.ToUnit,
		File:        filepath.ToSlash(gr.File),
		Start:       gr.Span[0],
		End:         gr.Span[1],
	}, nil
}

func convertGoDoc(gd *gog.Doc) (*graph.Doc, error) {
	var key graph.DefKey
	if gd.DefKey != nil {
		resolvedTarget, err := ResolveDep(gd.PackageImportPath)
		if err != nil {
			return nil, err
		}
		key = graph.DefKey{
			Path:     filepath.ToSlash(pathOrDot(filepath.Join(gd.Path...))),
			Unit:     resolvedTarget.ToUnit,
			UnitType: resolvedTarget.ToUnitType,
		}
	}

	resolvedDocUnit, err := ResolveDep(gd.Unit)
	if err != nil {
		return nil, err
	}
	if resolvedDocUnit == nil {
		return nil, nil
	}

	return &graph.Doc{
		DefKey:  key,
		Format:  gd.Format,
		Data:    gd.Data,
		File:    filepath.ToSlash(gd.File),
		Start:   gd.Span[0],
		End:     gd.Span[1],
		DocUnit: resolvedDocUnit.ToUnit,
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

func treePath(path string) string {
	if path == "" || path == "." {
		return string(".")
	}
	return "./" + path
}

func doGraph(pkg *build.Package) (*gog.Output, error) {
	var allGoFiles []string
	allGoFiles = append(allGoFiles, pkg.GoFiles...)
	allGoFiles = append(allGoFiles, pkg.CgoFiles...)
	allGoFiles = append(allGoFiles, pkg.TestGoFiles...)
	if len(allGoFiles) == 0 {
		return &gog.Output{}, nil
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range allGoFiles {
		file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	typesConfig := &types.Config{
		Importer: &buildContextImporter{
			context: &buildContext,
			srcDir:  pkg.Dir,
			packages: map[string]*types.Package{
				"unsafe": types.Unsafe,
			},
		},
		FakeImportC: true,
		Error: func(err error) {
			// errors are ignored, use best-effort type checking output
		},
	}
	typesInfo := &types.Info{
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
	typesPkg, err := typesConfig.Check(pkg.ImportPath, fset, files, typesInfo)
	if err != nil {
		log.Println("type checker error:", err) // see comment above
	}

	return gog.Graph(fset, files, typesPkg, typesInfo, true), nil
}

type buildContextImporter struct {
	context  *build.Context
	srcDir   string
	packages map[string]*types.Package
}

func (i *buildContextImporter) Import(path string) (*types.Package, error) {
	buildPkg, err := i.context.Import(path, i.srcDir, build.FindOnly&build.AllowBinary)
	if err != nil {
		return nil, err
	}

	if typesPkg, ok := i.packages[buildPkg.ImportPath]; ok && typesPkg.Complete() {
		return typesPkg, nil
	}

	if _, err := os.Stat(buildPkg.PkgObj); os.IsNotExist(err) {
		// try to build .a file if it does not exist
		cmd := exec.Command("go", "install", "-buildmode=archive", buildPkg.ImportPath)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GOROOT=" + i.context.GOROOT, "GOPATH=" + i.context.GOPATH, "CGO_ENABLED=0"}
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, err
		}
	}

	r, err := os.Open(buildPkg.PkgObj)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	br := bufio.NewReader(r)

	_, err = gcimporter.FindExportData(br)
	if err != nil {
		return nil, err
	}

	typesPkg, err := gcimporter.ImportData(i.packages, buildPkg.PkgObj, buildPkg.ImportPath, br)
	if err != nil {
		return nil, err
	}

	return typesPkg, nil
}
