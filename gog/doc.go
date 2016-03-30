package gog

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"

	"go/types"

	"golang.org/x/tools/go/loader"
)

type Doc struct {
	*DefKey

	Unit   string
	Format string
	Data   string

	File string    `json:",omitempty"`
	Span [2]uint32 `json:",omitempty"`
}

func parseFiles(fset *token.FileSet, filenames []string) (map[string]*ast.File, error) {
	files := make(map[string]*ast.File)
	for _, path := range filenames {
		// read file contents using go/build context so we use our vfs if
		// present
		var f io.ReadCloser
		var err error
		if build.Default.OpenFile != nil {
			f, err = build.Default.OpenFile(path)
		} else {
			f, err = os.Open(path)
		}
		if err != nil {
			return nil, err
		}
		defer f.Close()

		file, err := parser.ParseFile(fset, path, f, parser.ParseComments)
		if err != nil {
			// Don't fail on parser errors.
			log.Printf("Warning: parsing: %s.", err)
		}
		if file != nil {
			files[path] = file
		}
	}
	return files, nil
}

func (g *Grapher) emitDocs(pkgInfo *loader.PackageInfo) error {
	objOf := make(map[token.Position]types.Object, len(pkgInfo.Defs))
	for ident, obj := range pkgInfo.Defs {
		objOf[g.program.Fset.Position(ident.Pos())] = obj
	}

	var filenames []string
	for _, f := range pkgInfo.Files {
		name := g.program.Fset.Position(f.Name.Pos()).Filename
		if filepath.Base(name) == "C" {
			// skip cgo-generated file
			continue
		}
		if path.Ext(name) == ".go" {
			filenames = append(filenames, name)
		}
	}
	sort.Strings(filenames)
	files, err := parseFiles(g.program.Fset, filenames)
	if err != nil {
		return err
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

	pkgPath := pkgInfo.Pkg.Path()
	g.emitDoc(types.NewPkgName(0, pkgInfo.Pkg, pkgPath, pkgInfo.Pkg), nil, pkgDoc, "", pkgPath)

	// We walk the AST for comments attached to nodes.
	for filename, f := range files {
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
					if g.emitDoc(objOf[g.program.Fset.Position(i.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath) {
						docSeen[n.Doc.Pos()] = struct{}{}
					}
				}
			case *ast.FuncDecl:
				if n.Doc == nil || n.Name == nil {
					return true
				}
				if g.emitDoc(objOf[g.program.Fset.Position(n.Name.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath) {
					docSeen[n.Doc.Pos()] = struct{}{}
				}
			case *ast.GenDecl:
				for _, spec := range n.Specs {
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							c := firstNonNil(spec.Doc, spec.Comment, n.Doc)
							if g.emitDoc(objOf[g.program.Fset.Position(name.Pos())], c, c.Text(), filename, pkgPath) {
								docSeen[c.Pos()] = struct{}{}
							}
						}
					case *ast.TypeSpec:
						c := firstNonNil(spec.Doc, spec.Comment, n.Doc)
						if g.emitDoc(objOf[g.program.Fset.Position(spec.Name.Pos())], c, c.Text(), filename, pkgPath) {
							docSeen[c.Pos()] = struct{}{}
						}
					}
				}
			case *ast.ImportSpec:
				if n.Doc == nil || n.Name == nil {
					return true
				}
				if g.emitDoc(objOf[g.program.Fset.Position(n.Name.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath) {
					docSeen[n.Doc.Pos()] = struct{}{}
				}
			case *ast.TypeSpec:
				if n.Doc == nil || n.Name == nil {
					return true
				}
				if g.emitDoc(objOf[g.program.Fset.Position(n.Name.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath) {
					docSeen[n.Doc.Pos()] = struct{}{}
				}
			case *ast.ValueSpec:
				if n.Doc == nil {
					return true
				}
				for _, i := range n.Names {
					if g.emitDoc(objOf[g.program.Fset.Position(i.Pos())], n.Doc, n.Doc.Text(), filename, pkgPath) {
						docSeen[n.Doc.Pos()] = struct{}{}
					}
				}
			}
			return true
		})
		// Add comments that haven't already been seen.
		for _, c := range f.Comments {
			if _, seen := docSeen[c.Pos()]; !seen {
				g.emitDoc(nil, c, c.Text(), filename, pkgPath)
			}
		}
	}
	return nil
}

func firstNonNil(comments ...*ast.CommentGroup) *ast.CommentGroup {
	for _, c := range comments {
		if c != nil {
			return c
		}
	}
	return nil
}

func (g *Grapher) emitDoc(obj types.Object, dc *ast.CommentGroup, docstring, filename, pkgPath string) bool {
	if docstring == "" {
		return false
	}
	if obj == nil {
		var htmlBuf bytes.Buffer
		doc.ToHTML(&htmlBuf, docstring, nil)
		var span [2]uint32
		if dc != nil {
			span = makeSpan(g.program.Fset, dc)
		}
		g.addDoc(&Doc{
			DefKey: nil,
			Unit:   pkgPath,
			Format: "text/html",
			Data:   htmlBuf.String(),
			File:   filename,
			Span:   span,
		})
		g.addDoc(&Doc{
			DefKey: nil,
			Unit:   pkgPath,
			Format: "text/plain",
			Data:   docstring,
			File:   filename,
			Span:   span,
		})
		return true
	}

	if g.seenDocObjs == nil {
		g.seenDocObjs = make(map[types.Object]struct{})
	}
	if _, seen := g.seenDocObjs[obj]; seen {
		return false
	}
	g.seenDocObjs[obj] = struct{}{}

	key, _, err := g.defInfo(obj)
	if err != nil {
		return false
	}

	if g.seenDocKeys == nil {
		g.seenDocKeys = make(map[string]struct{})
	}
	if _, seen := g.seenDocKeys[key.String()]; seen {
		return false
	}
	g.seenDocKeys[key.String()] = struct{}{}

	var htmlBuf bytes.Buffer
	doc.ToHTML(&htmlBuf, docstring, nil)

	var span [2]uint32
	if dc != nil {
		span = makeSpan(g.program.Fset, dc)
	}

	g.addDoc(&Doc{
		DefKey: key,
		Unit:   pkgPath,
		Format: "text/html",
		Data:   htmlBuf.String(),
		File:   filename,
		Span:   span,
	})
	g.addDoc(&Doc{
		DefKey: key,
		Unit:   pkgPath,
		Format: "text/plain",
		Data:   docstring,
		File:   filename,
		Span:   span,
	})

	return true
}

func (g *Grapher) addDoc(doc *Doc) {
	g.Docs = append(g.Docs, doc)
}
