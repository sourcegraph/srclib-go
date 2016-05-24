package gog

import (
	"bytes"
	"go/ast"
	"go/doc"
	"go/token"

	"go/types"
)

type Doc struct {
	*DefKey

	Unit   string
	Format string
	Data   string

	File string    `json:",omitempty"`
	Span [2]uint32 `json:",omitempty"`
}

func (g *Grapher) emitDocs(files []*ast.File, typesPkg *types.Package, typesInfo *types.Info) ([]*Doc, error) {
	var pkgDocs []*Doc
	objOf := make(map[token.Position]types.Object, len(typesInfo.Defs))
	for ident, obj := range typesInfo.Defs {
		objOf[g.fset.Position(ident.Pos())] = obj
	}

	// First we collect all of the Doc comments from the files,
	// which will make up the Doc for the package. If more than
	// one file has a doc associated with it, append them
	// together.
	pkgDoc := ""
	for _, f := range files {
		if f.Doc == nil {
			continue
		}
		if pkgDoc == "" {
			pkgDoc = f.Doc.Text()
			continue
		}
		pkgDoc += "\n" + f.Doc.Text()
	}

	pkgPath := typesPkg.Path()
	fileDocs := g.emitDoc(types.NewPkgName(0, typesPkg, pkgPath, typesPkg), nil, pkgDoc, "", pkgPath)
	pkgDocs = append(pkgDocs, fileDocs...)

	// We walk the AST for comments attached to nodes.
	for _, f := range files {
		filename := g.fset.Position(f.Name.Pos()).Filename
		// docSeen is a map from the starting byte of a doc to
		// an empty struct.
		docSeen := make(map[token.Pos]struct{})
		ast.Inspect(f, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.Field:
				if n.Doc == nil {
					return true
				}
				for _, i := range n.Names {
					if docs := g.emitDoc(objOf[g.fset.Position(i.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath); len(docs) > 0 {
						docSeen[n.Doc.Pos()] = struct{}{}
						pkgDocs = append(pkgDocs, docs...)
					}
				}
			case *ast.FuncDecl:
				if n.Doc == nil || n.Name == nil {
					return true
				}
				if docs := g.emitDoc(objOf[g.fset.Position(n.Name.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath); len(docs) > 0 {
					docSeen[n.Doc.Pos()] = struct{}{}
					pkgDocs = append(pkgDocs, docs...)
				}
			case *ast.GenDecl:
				for _, spec := range n.Specs {
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							c := firstNonNil(spec.Doc, spec.Comment, n.Doc)
							if docs := g.emitDoc(objOf[g.fset.Position(name.Pos())], c, c.Text(), filename, pkgPath); len(docs) > 0 {
								docSeen[c.Pos()] = struct{}{}
								pkgDocs = append(pkgDocs, docs...)
							}
						}
					case *ast.TypeSpec:
						c := firstNonNil(spec.Doc, spec.Comment, n.Doc)
						if docs := g.emitDoc(objOf[g.fset.Position(spec.Name.Pos())], c, c.Text(), filename, pkgPath); len(docs) > 0 {
							docSeen[c.Pos()] = struct{}{}
							pkgDocs = append(pkgDocs, docs...)
						}
					}
				}
			case *ast.ImportSpec:
				if n.Doc == nil || n.Name == nil {
					return true
				}
				if docs := g.emitDoc(objOf[g.fset.Position(n.Name.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath); len(docs) > 0 {
					docSeen[n.Doc.Pos()] = struct{}{}
					pkgDocs = append(pkgDocs, docs...)
				}
			case *ast.TypeSpec:
				if n.Doc == nil || n.Name == nil {
					return true
				}
				if docs := g.emitDoc(objOf[g.fset.Position(n.Name.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath); len(docs) > 0 {
					docSeen[n.Doc.Pos()] = struct{}{}
					pkgDocs = append(pkgDocs, docs...)
				}
			case *ast.ValueSpec:
				if n.Doc == nil {
					return true
				}
				for _, i := range n.Names {
					if docs := g.emitDoc(objOf[g.fset.Position(i.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath); len(docs) > 0 {
						docSeen[n.Doc.Pos()] = struct{}{}
						pkgDocs = append(pkgDocs, docs...)
					}
				}
			}
			return true
		})
		// Add comments that haven't already been seen.
		for _, c := range f.Comments {
			if _, seen := docSeen[c.Pos()]; !seen {
				commentDocs := g.emitDoc(nil, c, c.Text(), filename, pkgPath)
				pkgDocs = append(pkgDocs, commentDocs...)
			}
		}
	}
	return pkgDocs, nil
}

func firstNonNil(comments ...*ast.CommentGroup) *ast.CommentGroup {
	for _, c := range comments {
		if c != nil {
			return c
		}
	}
	return nil
}

func (g *Grapher) emitDoc(obj types.Object, dc *ast.CommentGroup, docstring, filename, pkgPath string) (docs []*Doc) {
	if docstring == "" {
		return
	}
	if obj == nil {
		var htmlBuf bytes.Buffer
		doc.ToHTML(&htmlBuf, docstring, nil)
		var span [2]uint32
		if dc != nil {
			span = makeSpan(g.fset, dc)
		}
		docs = append(docs, &Doc{
			DefKey: nil,
			Unit:   pkgPath,
			Format: "text/html",
			Data:   htmlBuf.String(),
			File:   filename,
			Span:   span,
		})
		docs = append(docs, &Doc{
			DefKey: nil,
			Unit:   pkgPath,
			Format: "text/plain",
			Data:   docstring,
			File:   filename,
			Span:   span,
		})
		return
	}

	if g.seenDocObjs == nil {
		g.seenDocObjs = make(map[types.Object]struct{})
	}
	if _, seen := g.seenDocObjs[obj]; seen {
		return
	}
	g.seenDocObjs[obj] = struct{}{}

	key, _ := g.defInfo(obj)

	if g.seenDocKeys == nil {
		g.seenDocKeys = make(map[string]struct{})
	}
	if _, seen := g.seenDocKeys[key.String()]; seen {
		return
	}
	g.seenDocKeys[key.String()] = struct{}{}

	var htmlBuf bytes.Buffer
	doc.ToHTML(&htmlBuf, docstring, nil)

	var span [2]uint32
	if dc != nil {
		span = makeSpan(g.fset, dc)
	}

	docs = append(docs, &Doc{
		DefKey: key,
		Unit:   pkgPath,
		Format: "text/html",
		Data:   htmlBuf.String(),
		File:   filename,
		Span:   span,
	})
	docs = append(docs, &Doc{
		DefKey: key,
		Unit:   pkgPath,
		Format: "text/plain",
		Data:   docstring,
		File:   filename,
		Span:   span,
	})
	return
}
