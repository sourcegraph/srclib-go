package gog

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"strings"

	"code.google.com/p/go.tools/go/loader"
	"code.google.com/p/go.tools/go/types"
)

type DefKey struct {
	PackageImportPath string
	Path              []string
}

func (s *DefKey) String() string {
	return s.PackageImportPath + "#" + strings.Join(s.Path, ".")
}

type Def struct {
	Name string

	*DefKey

	File      string
	IdentSpan [2]int
	DeclSpan  [2]int

	DefInfo
}

type DefInfo struct {
	// Exported is whether this def is exported.
	Exported bool `json:",omitempty"`

	// PkgScope is whether this def is in Go package scope.
	PkgScope bool `json:",omitempty"`

	// PkgName is the name (not import path) of the package containing this
	// def.
	PkgName string

	// Receiver is the receiver of this def (or the empty string if this
	// def is not a method).
	Receiver string `json:",omitempty"`

	// FieldOfStruct is the struct that this def is a field of (or the empty string if this
	// def is not a struct field).
	FieldOfStruct string `json:",omitempty"`

	// TypeString is a string describing this def's Go type.
	TypeString string

	// UnderlyingTypeString is the function or method signature, if this is a function or method.
	UnderlyingTypeString string `json:",omitempty"`

	// Kind is the kind of Go thing this def is: struct, interface, func,
	// package, etc.
	Kind string `json:",omitempty"`
}

// NewDef creates a new Def.
func (g *Grapher) NewDef(obj types.Object, declIdent *ast.Ident) (*Def, error) {
	// Find the AST node that declares this def.
	var declNode ast.Node
	_, astPath, _ := g.program.PathEnclosingInterval(declIdent.Pos(), declIdent.End())
	for _, node := range astPath {
		switch node.(type) {
		case *ast.FuncDecl, *ast.GenDecl, *ast.ValueSpec, *ast.TypeSpec, *ast.Field, *ast.DeclStmt, *ast.AssignStmt:
			declNode = node
			goto found
		}
	}
found:
	if declNode == nil {
		return nil, fmt.Errorf("On ident %s at %s: no DeclNode found (using PathEnclosingInterval)", declIdent.Name, g.program.Fset.Position(declIdent.Pos()))
	}

	key, info, err := g.defInfo(obj)
	if err != nil {
		return nil, err
	}

	si := DefInfo{
		Exported: info.exported,
		PkgScope: info.pkgscope,
		PkgName:  obj.Pkg().Name(),
		Kind:     defKind(obj),
	}

	if typ := obj.Type(); typ != nil {
		si.TypeString = typ.String()
		if utyp := typ.Underlying(); utyp != nil {
			si.UnderlyingTypeString = utyp.String()
		}
	}

	switch obj := obj.(type) {
	case *types.Var:
		if obj.IsField() {
			if fieldStruct, ok := g.structFields[obj]; ok {
				if struct_, ok := fieldStruct.parent.(*types.Named); ok {
					si.FieldOfStruct = struct_.Obj().Name()
				}
			}
		}
	case *types.Func:
		sig := obj.Type().(*types.Signature)
		if recv := sig.Recv(); recv != nil && recv.Type() != nil {
			// omit package path; just get receiver type name
			si.Receiver = strings.Replace(recv.Type().String(), obj.Pkg().Path()+".", "", 1)
		}
	}

	return &Def{
		Name: obj.Name(),

		DefKey: key,

		File:      g.program.Fset.Position(declIdent.Pos()).Filename,
		IdentSpan: makeSpan(g.program.Fset, declIdent),
		DeclSpan:  makeSpan(g.program.Fset, declNode),

		DefInfo: si,
	}, nil
}

// NewPackageDef creates a new Def that represents a Go package.
func (g *Grapher) NewPackageDef(pkgInfo *loader.PackageInfo, pkg *types.Package) (*Def, error) {
	var pkgDir string
	if len(pkgInfo.Files) > 0 {
		pkgDir = filepath.Dir(g.program.Fset.Position(pkgInfo.Files[0].Package).Filename)
	}

	return &Def{
		Name: pkg.Name(),

		DefKey: &DefKey{PackageImportPath: pkg.Path(), Path: []string{}},

		File: pkgDir,

		DefInfo: DefInfo{
			Exported: true,
			PkgName:  pkg.Name(),
			Kind:     Package,
		},
	}, nil
}

func defKind(obj types.Object) string {
	switch obj := obj.(type) {
	case *types.PkgName:
		return Package
	case *types.Const:
		return Const
	case *types.TypeName:
		return Type
	case *types.Var:
		if obj.IsField() {
			return Field
		}
		return Var
	case *types.Func:
		sig := obj.Type().(*types.Signature)
		if sig.Recv() == nil {
			return Func
		} else {
			return Method
		}
	default:
		panic(fmt.Sprintf("unhandled obj type %T", obj))
	}
}
