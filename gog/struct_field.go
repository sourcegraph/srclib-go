package gog

import "go/types"

type structField struct {
	*types.Var
	parent types.Type
}

func (g *Grapher) buildStructFields(typesInfo *types.Info) {
	for _, obj := range typesInfo.Defs {
		if tn, ok := obj.(*types.TypeName); ok {
			typ := tn.Type().Underlying()
			if st, ok := typ.(*types.Struct); ok {
				for i := 0; i < st.NumFields(); i++ {
					sf := st.Field(i)
					g.structFields[sf] = &structField{sf, tn.Type()}
				}
			}
		}
	}

	for selExpr, sel := range typesInfo.Selections {
		switch sel.Kind() {
		case types.FieldVal:
			rt := derefType(sel.Recv())
			var pkg *types.Package
			switch rt := rt.(type) {
			case *types.Named:
				pkg = rt.Obj().Pkg()
			case *types.Struct:
				pkg = sel.Obj().Pkg()
			default:
				panic("unhandled field recv type " + rt.String())
			}
			sfobj, _, _ := types.LookupFieldOrMethod(derefType(sel.Recv()), false, pkg, selExpr.Sel.Name)

			// Record that this field is in this struct so we can construct the
			// right def path to the field.
			sf, _ := sfobj.(*types.Var)
			g.structFields[sf] = &structField{sf, rt}
		}
	}
}
