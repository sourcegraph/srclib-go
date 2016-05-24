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

type Grapher struct {
	SkipDocs bool

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
	scopePaths map[*types.Scope][]string
	pkgscope   map[types.Object]bool
	fields     map[*types.Var]types.Type

	Output

	seenDocObjs map[types.Object]struct{}
	seenDocKeys map[string]struct{}
}

func New() *Grapher {
	g := &Grapher{
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

func (g *Grapher) Graph(fset *token.FileSet, files []*ast.File, typesPkg *types.Package, typesInfo *types.Info) {
	if len(files) == 0 {
		log.Printf("warning: attempted to graph package %s with no files", typesPkg.Path())
		return
	}

	g.fset = fset
	g.files = files
	g.typesPkg = typesPkg
	g.typesInfo = typesInfo
	g.buildScopeInfo(typesInfo)
	g.assignPathsInPackage(typesPkg)

	// Accumulate the defs, refs and docs from the package being graphed currently.
	// If the package is graphed successfully, these are added to Output.
	v := &astVisitor{g: g, typesPkg: typesPkg, typesInfo: typesInfo}

	v.pkgDefs = append(v.pkgDefs, g.NewPackageDef(filepath.Dir(g.fset.Position(files[0].Package).Filename), typesPkg))

	for _, f := range files {
		ast.Walk(v, f)
	}

	if !g.SkipDocs {
		v.pkgDocs = g.emitDocs(files, typesPkg, typesInfo)
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

	pkgDefs []*Def
	pkgRefs []*Ref
	pkgDocs []*Doc

	structName string
}

func (v *astVisitor) Visit(node ast.Node) (w ast.Visitor) {
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
		v.newDef(n, n.Name)
		if s, ok := n.Type.(*ast.StructType); ok {
			ast.Walk(v, n.Name)
			v.structName = n.Name.Name
			ast.Walk(v, s)
			v.structName = ""
			return nil
		}

	case *ast.Field:
		v.newDef(n, n.Type) // anonymous field
		if starExpr, ok := n.Type.(*ast.StarExpr); ok {
			v.newDef(n, starExpr.X) // anonymous field with pointer
		}
		for _, name := range n.Names {
			v.newDef(n, name)
		}

	case *ast.FuncDecl:
		v.newDef(n, n.Name)

	case *ast.ValueSpec:
		for _, name := range n.Names {
			v.newDef(n, name)
		}

	case *ast.AssignStmt:
		for _, lhs := range n.Lhs {
			v.newDef(n, lhs)
		}

	case *ast.RangeStmt:
		v.newDef(n, n.Key)
		v.newDef(n, n.Value)

	case *ast.SelectorExpr:
		if sel := v.typesInfo.Selections[n]; sel != nil {
			t := sel.Recv()
			if ptr, ok := t.(*types.Pointer); ok {
				t = ptr.Elem()
			}
			v.g.fields[sel.Obj().(*types.Var)] = t
		}

	case *ast.Ident:
		if n.Name == "_" {
			break
		}
		if obj := v.typesInfo.ObjectOf(n); obj != nil {
			ref := v.g.NewRef(n, obj, v.typesPkg.Path())
			ref.IsDef = (v.typesInfo.Defs[n] != nil)
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

func (v *astVisitor) newDef(declNode, declName ast.Node) {
	declIdent, ok := declName.(*ast.Ident)
	if !ok || declIdent.Name == "_" {
		return
	}

	obj := v.typesInfo.Defs[declIdent]
	if obj == nil {
		return
	}

	v.pkgDefs = append(v.pkgDefs, v.g.NewDef(obj, declNode, declIdent, v.structName))
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
		p := g.fset.Position(obj.Pos())
		path = append([]string{filepath.Base(p.Filename)}, path...)
	}

	return &DefKey{obj.Pkg().Path(), path}, &defInfo{
		pkgscope: g.pkgscope[obj],
		exported: obj.Exported() && (obj.Parent() == nil || obj.Parent() == obj.Pkg().Scope()),
	}
}
