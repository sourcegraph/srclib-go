package gog

import (
	"go/ast"
	"go/constant"
	"log"
	"path/filepath"
	"sort"
	"sync"

	"go/types"

	_ "golang.org/x/tools/go/gcimporter15"
	"golang.org/x/tools/go/loader"
)

type Output struct {
	Defs []*Def
	Refs []*Ref
	Docs []*Doc
}

type Grapher struct {
	SkipDocs bool

	program *loader.Program
	pkg     *types.Package

	defCacheLock sync.Mutex
	defInfoCache map[types.Object]*defInfo
	defKeyCache  map[types.Object]*DefKey

	scopeNodes map[*types.Scope]ast.Node
	funcNames  map[*types.Scope]string

	paths      map[types.Object][]string
	scopePaths map[*types.Scope][]string
	pkgscope   map[types.Object]bool
	fields     map[*types.Var]types.Type

	Output

	seenDocObjs map[types.Object]struct{}
	seenDocKeys map[string]struct{}
}

func New(prog *loader.Program) *Grapher {
	g := &Grapher{
		program:      prog,
		defInfoCache: make(map[types.Object]*defInfo),
		defKeyCache:  make(map[types.Object]*DefKey),

		scopeNodes: make(map[*types.Scope]ast.Node),
		funcNames:  make(map[*types.Scope]string),

		paths:      make(map[types.Object][]string),
		scopePaths: make(map[*types.Scope][]string),
		pkgscope:   make(map[types.Object]bool),
		fields:     make(map[*types.Var]types.Type),
	}

	return g
}

func sortedPkgs(m map[*types.Package]*loader.PackageInfo) []*loader.PackageInfo {
	var pis []*loader.PackageInfo
	for _, pi := range m {
		pis = append(pis, pi)
	}
	sort.Sort(packageInfos(pis))
	return pis
}

type packageInfos []*loader.PackageInfo

func (pi packageInfos) Len() int           { return len(pi) }
func (pi packageInfos) Less(i, j int) bool { return pi[i].Pkg.Path() < pi[j].Pkg.Path() }
func (pi packageInfos) Swap(i, j int)      { pi[i], pi[j] = pi[j], pi[i] }

func (g *Grapher) Graph(files []*ast.File, typesPkg *types.Package, typesInfo *types.Info) error {
	if len(files) == 0 {
		log.Printf("warning: attempted to graph package %s with no files", typesPkg.Path())
		return nil
	}

	g.pkg = typesPkg
	g.buildScopeInfo(typesInfo)
	g.assignPathsInPackage(typesPkg)

	// Accumulate the defs, refs and docs from the package being graphed currently.
	// If the package is graphed successfully, these are added to Output.
	v := &astVisitor{g: g, typesPkg: typesPkg, typesInfo: typesInfo}

	pkgDef, err := g.NewPackageDef(filepath.Dir(g.program.Fset.Position(files[0].Package).Filename), typesPkg)
	if err != nil {
		return err
	}
	v.pkgDefs = append(v.pkgDefs, pkgDef)

	for _, f := range files {
		ast.Walk(v, f)
	}
	if v.err != nil {
		return v.err
	}

	if !g.SkipDocs {
		var err error
		v.pkgDocs, err = g.emitDocs(files, typesPkg, typesInfo)
		if err != nil {
			return err
		}
	}

	// Transfer pkg graph data to output
	g.Defs = append(g.Defs, v.pkgDefs...)
	g.Refs = append(g.Refs, v.pkgRefs...)
	g.Docs = append(g.Docs, v.pkgDocs...)
}

type astVisitor struct {
	g         *Grapher
	typesPkg  *types.Package
	typesInfo *types.Info

	err     error
	pkgDefs []*Def
	pkgRefs []*Ref
	pkgDocs []*Doc

	structName string
}

func (v *astVisitor) Visit(node ast.Node) (w ast.Visitor) {
	if v.err != nil {
		return nil
	}

	switch n := node.(type) {
	case *ast.File:
		// Create a ref that represent the name of the package ("package foo")
		pkgObj := types.NewPkgName(n.Name.Pos(), v.typesPkg, v.typesPkg.Name(), v.typesPkg)
		ref := v.g.NewRef(n.Name, pkgObj, v.typesPkg.Path())
		v.pkgRefs = append(v.pkgRefs, ref)

	case *ast.ImportSpec:
		if obj := v.typesInfo.Implicits[n]; obj != nil {
			ref := v.g.NewRef(n, obj, v.typesPkg.Path())
			v.pkgRefs = append(v.pkgRefs, ref)
		}

	case *ast.TypeSpec:
		if s, ok := n.Type.(*ast.StructType); ok {
			ast.Walk(v, n.Name)
			v.structName = n.Name.Name
			ast.Walk(v, s)
			v.structName = ""
			return nil
		}

	case *ast.SelectorExpr:
		if sel := v.typesInfo.Selections[n]; sel != nil {
			t := sel.Recv()
			if ptr, ok := t.(*types.Pointer); ok {
				t = ptr.Elem()
			}
			v.g.fields[sel.Obj().(*types.Var)] = t
		}

	case *ast.Ident:
		ident := n
		if ident.Name == "_" {
			break
		}

		isDef := false
		if obj := v.typesInfo.Defs[ident]; obj != nil {
			isDef = true
			// don't treat import aliases as things that belong to this package
			if _, isPkg := obj.(*types.PkgName); !isPkg {
				def, err := v.g.NewDef(obj, ident, v.structName)
				if err != nil {
					v.err = err
					return nil
				}
				v.pkgDefs = append(v.pkgDefs, def)
			}
		}

		if obj := v.typesInfo.ObjectOf(ident); obj != nil {
			ref := v.g.NewRef(ident, obj, v.typesPkg.Path())
			ref.IsDef = isDef
			v.pkgRefs = append(v.pkgRefs, ref)
		}

	case *ast.LabeledStmt:
		ast.Walk(v, n.Stmt)
		return nil

	case *ast.BranchStmt:
		return nil
	}

	return v
}

type defInfo struct {
	exported bool
	pkgscope bool
}

func (g *Grapher) defInfo(obj types.Object) (*DefKey, *defInfo) {
	g.defCacheLock.Lock()
	key := g.defKeyCache[obj]
	info := g.defInfoCache[obj]

	if key == nil || info == nil {
		// Don't block while we traverse the AST to construct the object path. We
		// might duplicate effort, but it's better than allowing only one goroutine
		// to do this at a time.
		g.defCacheLock.Unlock()
		key, info = g.makeDefInfo(obj)
		g.defCacheLock.Lock()
		g.defKeyCache[obj] = key
		g.defInfoCache[obj] = info
	}

	g.defCacheLock.Unlock()
	return key, info
}

func (g *Grapher) makeDefInfo(obj types.Object) (*DefKey, *defInfo) {
	switch obj := obj.(type) {
	case *types.Builtin:
		return &DefKey{"builtin", []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}
	case *types.Nil:
		return &DefKey{"builtin", []string{"nil"}}, &defInfo{pkgscope: false, exported: true}
	case *types.TypeName:
		if basic, ok := obj.Type().(*types.Basic); ok {
			return &DefKey{"builtin", []string{basic.Name()}}, &defInfo{pkgscope: false, exported: true}
		}
		if obj.Name() == "error" {
			return &DefKey{"builtin", []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}
		}
	case *types.PkgName:
		return &DefKey{obj.Imported().Path(), []string{}}, &defInfo{pkgscope: false, exported: true}
	case *types.Const:
		var pkg string
		if obj.Pkg() == nil {
			pkg = "builtin"
		} else {
			pkg = obj.Pkg().Path()
		}
		if obj.Val().Kind() == constant.Bool && pkg == "builtin" {
			return &DefKey{pkg, []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}
		}
	}

	if obj.Pkg() == nil {
		// builtin
		return &DefKey{"builtin", []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}
	}

	path := g.path(obj)

	// Handle the case where a dir has 2 main packages that are not
	// intended to be compiled together and have overlapping def
	// paths. Prefix the def path with the filename.
	if obj.Pkg().Name() == "main" {
		p := g.program.Fset.Position(obj.Pos())
		path = append([]string{filepath.Base(p.Filename)}, path...)
	}

	return &DefKey{obj.Pkg().Path(), path}, &defInfo{
		pkgscope: g.pkgscope[obj],
		exported: obj.Exported() && (obj.Parent() == nil || obj.Parent() == obj.Pkg().Scope()),
	}
}
