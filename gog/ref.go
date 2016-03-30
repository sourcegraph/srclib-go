package gog

import (
	"go/ast"

	"go/types"
)

func (g *Grapher) NewRef(node ast.Node, obj types.Object, pkgPath string) (*Ref, error) {
	key, err := g.defKey(obj)
	if err != nil {
		return nil, err
	}

	pos := g.program.Fset.Position(node.Pos())
	return &Ref{
		Unit: pkgPath,
		File: pos.Filename,
		Span: makeSpan(g.program.Fset, node),
		Def:  key,
	}, nil
}

type Ref struct {
	Unit string
	File string
	Span [2]uint32
	Def  *DefKey

	// IsDef is true if ref is to the definition of Def, and false if it's to a
	// use of Def.
	IsDef bool
}
