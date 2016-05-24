package gog

import (
	"fmt"
	"go/ast"
	"strings"

	"sourcegraph.com/sourcegraph/srclib-go/gog/definfo"

	"go/types"
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
	IdentSpan [2]uint32
	DeclSpan  [2]uint32

	definfo.DefInfo
}

// NewDef creates a new Def.
func (g *grapher) NewDef(obj types.Object, declNode ast.Node, declIdent *ast.Ident, structName string) *Def {
	key, info := g.defInfo(obj)

	si := definfo.DefInfo{
		Exported:      info.exported,
		PkgScope:      info.pkgscope,
		PkgName:       obj.Pkg().Name(),
		Kind:          defKind(obj),
		FieldOfStruct: structName,
	}

	if typ := obj.Type(); typ != nil {
		si.TypeString = typ.String()
		if key.PackageImportPath == "builtin" {
			si.UnderlyingTypeString = "builtin"
		} else if utyp := typ.Underlying(); utyp != nil {
			si.UnderlyingTypeString = utyp.String()
		}
	}

	switch obj := obj.(type) {
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

		File:      g.fset.Position(declIdent.Pos()).Filename,
		IdentSpan: makeSpan(g.fset, declIdent),
		DeclSpan:  makeSpan(g.fset, declNode),

		DefInfo: si,
	}
}

// NewPackageDef creates a new Def that represents a Go package.
func (g *grapher) NewPackageDef(pkgDir string, pkg *types.Package) *Def {
	return &Def{
		Name: pkg.Name(),

		DefKey: &DefKey{PackageImportPath: pkg.Path(), Path: []string{}},

		File: pkgDir,

		DefInfo: definfo.DefInfo{
			Exported: true,
			PkgName:  pkg.Name(),
			Kind:     definfo.Package,
		},
	}
}

func defKind(obj types.Object) string {
	switch obj := obj.(type) {
	case *types.PkgName:
		return definfo.Package
	case *types.Const:
		return definfo.Const
	case *types.TypeName:
		return definfo.Type
	case *types.Var:
		if obj.IsField() {
			return definfo.Field
		}
		return definfo.Var
	case *types.Func:
		sig := obj.Type().(*types.Signature)
		if sig.Recv() == nil {
			return definfo.Func
		} else {
			return definfo.Method
		}
	default:
		panic(fmt.Sprintf("unhandled obj type %T", obj))
	}
}
