package gog

import (
	"go/ast"
	"go/constant"
	"go/token"
	"log"
	"path/filepath"
	"sync"

	"go/types"

	_ "golang.org/x/tools/go/gcimporter15"
)

type Output struct {
	Defs []*Def
	Refs []*Ref
	Docs []*Doc
}

func (o *Output) Append(o2 *Output) {
	o.Defs = append(o.Defs, o2.Defs...)
	o.Refs = append(o.Refs, o2.Refs...)
	o.Docs = append(o.Docs, o2.Docs...)
}

type grapher struct {
	fset      *token.FileSet
	files     []*ast.File
	typesPkg  *types.Package
	typesInfo *types.Info

	defCacheLock sync.Mutex
	defInfoCache map[types.Object]*defInfo
	defKeyCache  map[types.Object]*DefKey

	scopeNodes map[*types.Scope]ast.Node
	funcNames  map[*types.Scope]string

	paths      map[types.Object][]string
	usedPaths  map[string]struct{}
	scopePaths map[*types.Scope][]string
	pkgscope   map[types.Object]bool
	selRecvs   map[types.Object]types.Type
	structName string

	output *Output

	seenDocObjs map[types.Object]struct{}
	seenDocKeys map[string]struct{}
}

func Graph(fset *token.FileSet, files []*ast.File, typesPkg *types.Package, typesInfo *types.Info, includeDocs bool) *Output {
	if len(files) == 0 {
		log.Printf("warning: attempted to graph package %s with no files", typesPkg.Path())
		return &Output{}
	}

	g := &grapher{
		fset:      fset,
		files:     files,
		typesPkg:  typesPkg,
		typesInfo: typesInfo,

		defInfoCache: make(map[types.Object]*defInfo),
		defKeyCache:  make(map[types.Object]*DefKey),

		scopeNodes: make(map[*types.Scope]ast.Node),
		funcNames:  make(map[*types.Scope]string),

		paths:      make(map[types.Object][]string),
		usedPaths:  make(map[string]struct{}),
		scopePaths: make(map[*types.Scope][]string),
		pkgscope:   make(map[types.Object]bool),
		selRecvs:   make(map[types.Object]types.Type),

		output: &Output{},
	}

	g.buildScopeInfo(typesInfo)
	g.assignPathsInPackage(typesPkg)

	g.output.Defs = append(g.output.Defs, g.NewPackageDef(filepath.Dir(g.fset.Position(files[0].Package).Filename), typesPkg))

	for _, f := range files {
		ast.Walk(g, f)
	}

	if includeDocs {
		g.output.Docs = g.emitDocs(files, typesPkg, typesInfo)
	}

	return g.output
}

func (g *grapher) Visit(node ast.Node) (w ast.Visitor) {
	switch n := node.(type) {
	case *ast.File:
		// Create a ref that represent the name of the package ("package foo")
		pkgObj := types.NewPkgName(n.Name.Pos(), g.typesPkg, g.typesPkg.Name(), g.typesPkg)
		ref := g.NewRef(n.Name, pkgObj, g.typesPkg.Path())
		g.output.Refs = append(g.output.Refs, ref)

	case *ast.ImportSpec:
		if obj := g.typesInfo.Implicits[n]; obj != nil {
			ref := g.NewRef(n, obj, g.typesPkg.Path())
			g.output.Refs = append(g.output.Refs, ref)
		}

	case *ast.TypeSpec:
		g.newDef(n, n.Name)
		if s, ok := n.Type.(*ast.StructType); ok {
			ast.Walk(g, n.Name)
			g.structName = n.Name.Name
			ast.Walk(g, s)
			g.structName = ""
			return nil
		}

	case *ast.Field:
		g.newDef(n, n.Type) // anonymous field
		if starExpr, ok := n.Type.(*ast.StarExpr); ok {
			g.newDef(n, starExpr.X) // anonymous field with pointer
		}
		for _, name := range n.Names {
			g.newDef(n, name)
		}

	case *ast.FuncDecl:
		g.newDef(n, n.Name)

	case *ast.ValueSpec:
		for _, name := range n.Names {
			g.newDef(n, name)
		}

	case *ast.AssignStmt:
		for _, lhs := range n.Lhs {
			g.newDef(n, lhs)
		}

	case *ast.RangeStmt:
		g.newDef(n, n.Key)
		g.newDef(n, n.Value)

	case *ast.SelectorExpr:
		if sel := g.typesInfo.Selections[n]; sel != nil {
			recv := sel.Recv()
			index := sel.Index()
			for i := 0; i < len(index)-1; i++ {
				if ptr, ok := recv.Underlying().(*types.Pointer); ok {
					recv = ptr.Elem()
				}
				recv = recv.Underlying().(*types.Struct).Field(index[i]).Type()
			}
			g.selRecvs[sel.Obj()] = recv
		}

	case *ast.Ident:
		if n.Name == "_" {
			break
		}
		if obj := g.typesInfo.ObjectOf(n); obj != nil {
			ref := g.NewRef(n, obj, g.typesPkg.Path())
			ref.IsDef = (g.typesInfo.Defs[n] != nil)
			g.output.Refs = append(g.output.Refs, ref)
		}

	case *ast.LabeledStmt:
		ast.Walk(g, n.Stmt)
		return nil

	case *ast.BranchStmt:
		return nil
	}

	return g
}

func (g *grapher) newDef(declNode, declName ast.Node) {
	declIdent, ok := declName.(*ast.Ident)
	if !ok || declIdent.Name == "_" {
		return
	}

	obj := g.typesInfo.Defs[declIdent]
	if obj == nil {
		return
	}

	g.output.Defs = append(g.output.Defs, g.NewDef(obj, declNode, declIdent, g.structName))
}

type defInfo struct {
	exported bool
	pkgscope bool
}

func (g *grapher) defInfo(obj types.Object) (*DefKey, *defInfo) {
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

func (g *grapher) makeDefInfo(obj types.Object) (*DefKey, *defInfo) {
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
		p := g.fset.Position(obj.Pos())
		path = append([]string{filepath.Base(p.Filename)}, path...)
	}

	return &DefKey{obj.Pkg().Path(), path}, &defInfo{
		pkgscope: g.pkgscope[obj],
		exported: obj.Exported() && (obj.Parent() == nil || obj.Parent() == obj.Pkg().Scope()),
	}
}
