package gog

import (
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"math/rand"
	"path/filepath"
	"strings"

	"go/types"

	"golang.org/x/tools/go/loader"
)

func (g *Grapher) buildScopeInfo(pkgInfo *loader.PackageInfo) {
	// Precomputing funcNames now avoids an expensive lookup later on.
	for ident, obj := range pkgInfo.Defs {
		if funcType, ok := obj.(*types.Func); ok {
			g.funcNames[funcType.Scope()] = ident.Name
		}
	}

	for node, scope := range pkgInfo.Scopes {
		g.scopeNodes[scope] = node
	}
}

func (g *Grapher) path(obj types.Object) (path []string) {
	if path, present := g.paths[obj]; present {
		return path
	}

	var scope *types.Scope
	pkgInfo, astPath, _ := g.program.PathEnclosingInterval(obj.Pos(), obj.Pos())
	if astPath != nil {
		for _, node := range astPath {
			if s, hasScope := pkgInfo.Scopes[node]; hasScope {
				scope = s
			}
		}
	}
	if scope == nil {
		scope = obj.Parent()
	}

	if scope == nil {
		// TODO(sqs): make this actually handle cases like the one described in
		// https://github.com/sourcegraph/sourcegraph.com/issues/218
		log.Printf("Warning: no scope for object %s at pos %s", obj.String(), g.program.Fset.Position(obj.Pos()))
		return nil
	}

	prefix, hasPath := g.scopePaths[scope]
	if !hasPath {
		panic("no scope path for scope " + scope.String())
	}
	path = append([]string{}, prefix...)
	p := g.program.Fset.Position(obj.Pos())
	path = append(path, obj.Name()+uniqID(p))
	return path

	panic("no scope node for object " + obj.String())
}

func uniqID(p token.Position) string {
	return fmt.Sprintf("$%s%d", strippedFilename(p.Filename), p.Offset)
}

func (g *Grapher) scopePath(prefix []string, s *types.Scope) []string {
	if path, present := g.scopePaths[s]; present {
		return path
	}
	path := append(prefix, g.scopeLabel(s)...)
	g.scopePaths[s] = path
	return path
}

func (g *Grapher) scopeLabel(s *types.Scope) (path []string) {
	node, present := g.scopeNodes[s]
	if !present {
		// TODO(sqs): diagnose why this happens. See https://github.com/sourcegraph/sourcegraph.com/issues/163.
		// log.Printf("no node found for scope (giving a dummy label); scope is: %s", s.String())
		return []string{fmt.Sprintf("ERROR%d", rand.Intn(10000))}
	}

	switch n := node.(type) {
	case *ast.File:
		return []string{}

	case *ast.Package:
		return []string{}

	case *ast.FuncType:
		// Get func name, but treat each "init" func as separate
		// (because there can be multiple top-level init funcs in a Go
		// package).
		if name, exists := g.funcNames[s]; exists && name != "init" {
			return []string{name}
		} else {
			_, astPath, _ := g.program.PathEnclosingInterval(n.Pos(), n.End())
			if f, ok := astPath[0].(*ast.FuncDecl); ok {
				var path []string
				if f.Recv != nil {
					path = []string{derefNode(f.Recv.List[0].Type).(*ast.Ident).Name}
				}
				var uniqName string
				if f.Name.Name == "init" {
					// init function can appear multiple times in each file and
					// package, so need to uniquify it
					uniqName = f.Name.Name + uniqID(g.program.Fset.Position(f.Name.Pos()))
				} else {
					uniqName = f.Name.Name
				}
				path = append(path, uniqName)

				return path
			}
		}
	}

	// get this scope's index in parent
	// TODO(sqs): is it necessary to uniquify this here now that we're handling
	// init above?
	p := s.Parent()
	var prefix []string
	if fs, ok := g.scopeNodes[p].(*ast.File); ok {
		// avoid file scope collisions by using file index as well
		filename := g.program.Fset.Position(fs.Name.Pos()).Filename
		prefix = []string{fmt.Sprintf("$%s", strippedFilename(filename))}
	}
	for i := 0; i < p.NumChildren(); i++ {
		if p.Child(i) == s {
			filename := g.program.Fset.Position(node.Pos()).Filename
			return append(prefix, fmt.Sprintf("$%s%d", strippedFilename(filename), i))
		}
	}

	panic("unreachable")
}

func strippedFilename(filename string) string {
	return strings.TrimSuffix(filepath.Base(filename), ".go")
}

func (g *Grapher) assignPathsInPackage(pkgInfo *loader.PackageInfo) {
	pkg := pkgInfo.Pkg
	g.assignPaths(pkg.Scope(), []string{}, true)
}

func (g *Grapher) assignPaths(s *types.Scope, prefix []string, pkgscope bool) {
	g.scopePaths[s] = prefix

	for _, name := range s.Names() {
		e := s.Lookup(name)
		if _, seen := g.paths[e]; seen {
			continue
		}
		path := append(append([]string{}, prefix...), name)
		g.paths[e] = path
		g.exported[e] = ast.IsExported(name) && pkgscope
		g.pkgscope[e] = pkgscope

		if tn, ok := e.(*types.TypeName); ok {
			// methods
			named := tn.Type().(*types.Named)
			g.assignMethodPaths(named, path, pkgscope)

			// struct fields
			typ := derefType(tn.Type().Underlying())
			if styp, ok := typ.(*types.Struct); ok {
				g.assignStructFieldPaths(styp, path, pkgscope)
			}
		} else if v, ok := e.(*types.Var); ok {
			// struct fields if type is anonymous struct
			if styp, ok := derefType(v.Type()).(*types.Struct); ok {
				g.assignStructFieldPaths(styp, path, pkgscope)
			}
		}
	}

	seenChildPrefixes := map[string]struct{}{}
	for i := 0; i < s.NumChildren(); i++ {
		c := s.Child(i)
		childPrefix := prefix
		pkgscope := pkgscope

		if path := g.scopePath(prefix, c); path != nil {
			childPrefix = append([]string{}, path...)
			pkgscope = false
		}

		if len(childPrefix) >= 1 {
			// Ensure all child prefixes are unique. This is an issue when you
			// have, for example:
			//  func init() { x:=0;_=x};func init() { x:=0;_=x}
			// This is valid Go code but if we don't uniquify the two `x`s, they
			// will have the same paths.
			cp := strings.Join(childPrefix, "/")
			if _, seen := seenChildPrefixes[cp]; seen {
				childPrefix[len(childPrefix)-1] += fmt.Sprintf("$%d", i)
			}
			seenChildPrefixes[cp] = struct{}{}
		}

		g.assignPaths(c, childPrefix, pkgscope)
	}
}

func (g *Grapher) assignMethodPaths(named *types.Named, prefix []string, pkgscope bool) {
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		path := append(append([]string{}, prefix...), m.Name())
		g.paths[m] = path

		g.exported[m] = ast.IsExported(m.Name())
		g.pkgscope[m] = pkgscope

		if s := m.Scope(); s != nil {
			g.assignPaths(s, path, false)
		}
	}

	if iface, ok := named.Underlying().(*types.Interface); ok {
		for i := 0; i < iface.NumExplicitMethods(); i++ {
			m := iface.Method(i)
			path := append(append([]string{}, prefix...), m.Name())
			g.paths[m] = path

			g.exported[m] = ast.IsExported(m.Name())
			g.pkgscope[m] = pkgscope

			if s := m.Scope(); s != nil {
				g.assignPaths(s, path, false)
			}
		}
	}
}

func (g *Grapher) assignStructFieldPaths(styp *types.Struct, prefix []string, pkgscope bool) {
	for i := 0; i < styp.NumFields(); i++ {
		f := styp.Field(i)
		path := append(append([]string{}, prefix...), f.Name())
		g.paths[f] = path

		g.exported[f] = ast.IsExported(f.Name())
		g.pkgscope[f] = pkgscope

		// recurse to anonymous structs (named structs are assigned directly)
		// TODO(sqs): handle arrays of structs, etc.
		if styp, ok := derefType(f.Type()).(*types.Struct); ok {
			g.assignStructFieldPaths(styp, path, pkgscope)
		}
	}
}
