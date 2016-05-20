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

	defCacheLock sync.Mutex
	defInfoCache map[types.Object]*defInfo
	defKeyCache  map[types.Object]*DefKey

	structFields map[*types.Var]*structField

	scopeNodes map[*types.Scope]ast.Node
	funcNames  map[*types.Scope]string

	paths      map[types.Object][]string
	scopePaths map[*types.Scope][]string
	exported   map[types.Object]bool
	pkgscope   map[types.Object]bool

	Output

	// skipResolve is the set of *ast.Idents that the grapher encountered but
	// did not resolve (by design). Idents in this set are omitted from the list
	// of unresolved idents in the tests.
	skipResolve map[*ast.Ident]struct{}

	seenDocObjs map[types.Object]struct{}
	seenDocKeys map[string]struct{}
}

func New(prog *loader.Program) *Grapher {
	g := &Grapher{
		program:      prog,
		defInfoCache: make(map[types.Object]*defInfo),
		defKeyCache:  make(map[types.Object]*DefKey),

		structFields: make(map[*types.Var]*structField),

		scopeNodes: make(map[*types.Scope]ast.Node),
		funcNames:  make(map[*types.Scope]string),

		paths:      make(map[types.Object][]string),
		scopePaths: make(map[*types.Scope][]string),
		exported:   make(map[types.Object]bool),
		pkgscope:   make(map[types.Object]bool),

		skipResolve: make(map[*ast.Ident]struct{}),
	}

	for _, pkgInfo := range sortedPkgs(prog.AllPackages) {
		g.buildStructFields(&pkgInfo.Info)
		g.buildScopeInfo(&pkgInfo.Info)
		g.assignPathsInPackage(pkgInfo.Pkg)
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

	seen := make(map[ast.Node]struct{})
	skipResolveObjs := make(map[types.Object]struct{})

	// Accumulate the defs, refs and docs from the package being graphed currently.
	// If the package is graphed successfully, these are added to Output.
	var pkgDefs []*Def
	var pkgRefs []*Ref
	var pkgDocs []*Doc

	for node, obj := range typesInfo.Implicits {
		if importSpec, ok := node.(*ast.ImportSpec); ok {
			ref, err := g.NewRef(importSpec, obj, typesPkg.Path())
			if err != nil {
				return err
			}
			pkgRefs = append(pkgRefs, ref)
			seen[importSpec] = struct{}{}
		} else if x, ok := node.(*ast.Ident); ok {
			g.skipResolve[x] = struct{}{}
		} else if _, ok := node.(*ast.CaseClause); ok {
			// type-specific *Var for each type switch case clause
			skipResolveObjs[obj] = struct{}{}
		}
	}

	pkgDef, err := g.NewPackageDef(filepath.Dir(g.program.Fset.Position(files[0].Package).Filename), typesPkg)
	if err != nil {
		return err
	}
	pkgDefs = append(pkgDefs, pkgDef)

	for ident, obj := range typesInfo.Defs {
		_, isLabel := obj.(*types.Label)
		if obj == nil || ident.Name == "_" || isLabel {
			g.skipResolve[ident] = struct{}{}
			continue
		}

		if v, isVar := obj.(*types.Var); isVar && obj.Pos() != ident.Pos() && !v.IsField() {
			// If this is an assign statement reassignment of existing var, treat this as a
			// use (not a def).
			typesInfo.Uses[ident] = obj
			continue
		}

		// don't treat import aliases as things that belong to this package
		_, isPkg := obj.(*types.PkgName)

		if !isPkg {
			def, err := g.NewDef(obj, ident)
			if err != nil {
				return err
			}
			pkgDefs = append(pkgDefs, def)
		}

		ref, err := g.NewRef(ident, obj, typesPkg.Path())
		if err != nil {
			return err
		}
		ref.IsDef = true
		pkgRefs = append(pkgRefs, ref)
	}

	for ident, obj := range typesInfo.Uses {
		if _, isLabel := obj.(*types.Label); isLabel {
			g.skipResolve[ident] = struct{}{}
			continue
		}

		if obj == nil || ident == nil || ident.Name == "_" {
			continue
		}

		if _, skip := skipResolveObjs[obj]; skip {
			g.skipResolve[ident] = struct{}{}
		}

		if _, seen := seen[ident]; seen {
			continue
		}

		if _, isLabel := obj.(*types.Label); isLabel {
			continue
		}

		ref, err := g.NewRef(ident, obj, typesPkg.Path())
		if err != nil {
			return err
		}
		pkgRefs = append(pkgRefs, ref)
	}

	// Create a ref that represent the name of the package ("package foo")
	// for each file.
	for _, f := range files {
		pkgObj := types.NewPkgName(f.Name.Pos(), typesPkg, typesPkg.Name(), typesPkg)
		ref, err := g.NewRef(f.Name, pkgObj, typesPkg.Path())
		if err != nil {
			return err
		}
		pkgRefs = append(pkgRefs, ref)
	}

	if !g.SkipDocs {
		pkgDocs, err = g.emitDocs(files, typesPkg, typesInfo)
		if err != nil {
			return err
		}
	}

	// Transfer pkg graph data to output
	g.Defs = append(g.Defs, pkgDefs...)
	g.Refs = append(g.Refs, pkgRefs...)
	g.Docs = append(g.Docs, pkgDocs...)

	return nil
}

type defInfo struct {
	exported bool
	pkgscope bool
}

func (g *Grapher) defKey(obj types.Object) (*DefKey, error) {
	key, _, err := g.defInfo(obj)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (g *Grapher) defInfo(obj types.Object) (*DefKey, *defInfo, error) {
	key, info := g.lookupDefInfo(obj)
	if key != nil && info != nil {
		return key, info, nil
	}

	// Don't block while we traverse the AST to construct the object path. We
	// might duplicate effort, but it's better than allowing only one goroutine
	// to do this at a time.

	key, info, err := g.makeDefInfo(obj)
	if err != nil {
		return nil, nil, err
	}

	g.defCacheLock.Lock()
	defer g.defCacheLock.Unlock()
	g.defKeyCache[obj] = key
	g.defInfoCache[obj] = info
	return key, info, nil
}

func (g *Grapher) lookupDefInfo(obj types.Object) (*DefKey, *defInfo) {
	g.defCacheLock.Lock()
	defer g.defCacheLock.Unlock()
	return g.defKeyCache[obj], g.defInfoCache[obj]
}

func (g *Grapher) makeDefInfo(obj types.Object) (*DefKey, *defInfo, error) {
	switch obj := obj.(type) {
	case *types.Builtin:
		return &DefKey{"builtin", []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}, nil
	case *types.Nil:
		return &DefKey{"builtin", []string{"nil"}}, &defInfo{pkgscope: false, exported: true}, nil
	case *types.TypeName:
		if basic, ok := obj.Type().(*types.Basic); ok {
			return &DefKey{"builtin", []string{basic.Name()}}, &defInfo{pkgscope: false, exported: true}, nil
		}
		if obj.Name() == "error" {
			return &DefKey{"builtin", []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}, nil
		}
	case *types.PkgName:
		return &DefKey{obj.Imported().Path(), []string{}}, &defInfo{pkgscope: false, exported: true}, nil
	case *types.Const:
		var pkg string
		if obj.Pkg() == nil {
			pkg = "builtin"
		} else {
			pkg = obj.Pkg().Path()
		}
		if obj.Val().Kind() == constant.Bool && pkg == "builtin" {
			return &DefKey{pkg, []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}, nil
		}
	}

	if obj.Pkg() == nil {
		// builtin
		return &DefKey{"builtin", []string{obj.Name()}}, &defInfo{pkgscope: false, exported: true}, nil
	}

	path := g.path(obj)

	// Handle the case where a dir has 2 main packages that are not
	// intended to be compiled together and have overlapping def
	// paths. Prefix the def path with the filename.
	if obj.Pkg().Name() == "main" {
		p := g.program.Fset.Position(obj.Pos())
		path = append([]string{filepath.Base(p.Filename)}, path...)
	}

	return &DefKey{obj.Pkg().Path(), path}, &defInfo{pkgscope: g.pkgscope[obj], exported: g.exported[obj]}, nil
}
