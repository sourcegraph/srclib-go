package gog

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types"
)

type Doc struct {
	*DefKey

	Format string
	Data   string

	File string `json:",omitempty"`
	Span [2]int `json:",omitempty"`
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
			return nil, err
		}
		files[path] = file
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
		filenames = append(filenames, name)
	}
	sort.Strings(filenames)
	files, err := parseFiles(g.program.Fset, filenames)
	if err != nil {
		return err
	}

	// ignore errors because we assume that syntax checking has already occurred
	astPkg, _ := ast.NewPackage(g.program.Fset, files, nil, nil)

	docPkg := doc.New(astPkg, pkgInfo.Pkg.Path(), doc.AllDecls)

	if docPkg.Doc != "" {
		err := g.emitDoc(types.NewPkgName(0, pkgInfo.Pkg, pkgInfo.Pkg.Path(), nil), nil, docPkg.Doc)
		if err != nil {
			return err
		}
	}

	emitDocForSpecs := func(pkgInfo *loader.PackageInfo, decl *ast.GenDecl, docstring string) error {
		for _, spec := range decl.Specs {
			switch spec := spec.(type) {
			case *ast.ValueSpec:
				for _, name := range spec.Names {
					g.emitDoc(objOf[g.program.Fset.Position(name.Pos())], firstNonNil(decl.Doc, spec.Doc, spec.Comment), docstring)
				}
			case *ast.TypeSpec:
				g.emitDoc(objOf[g.program.Fset.Position(spec.Name.Pos())], firstNonNil(decl.Doc, spec.Doc, spec.Comment), docstring)
			default:
				log.Panicf("unknown spec type %T", spec)
			}
		}

		return nil
	}

	for _, cnst := range docPkg.Consts {
		emitDocForSpecs(pkgInfo, cnst.Decl, cnst.Doc)
	}

	for _, vari := range docPkg.Vars {
		emitDocForSpecs(pkgInfo, vari.Decl, vari.Doc)
	}

	for _, fun := range docPkg.Funcs {
		g.emitDoc(objOf[g.program.Fset.Position(fun.Decl.Name.Pos())], fun.Decl.Doc, fun.Doc)
	}

	for _, typ := range docPkg.Types {
		emitDocForSpecs(pkgInfo, typ.Decl, typ.Doc)
		for _, cnst := range typ.Consts {
			emitDocForSpecs(pkgInfo, cnst.Decl, cnst.Doc)
		}
		for _, vari := range typ.Vars {
			emitDocForSpecs(pkgInfo, vari.Decl, vari.Doc)
		}
		for _, fun := range typ.Funcs {
			g.emitDoc(objOf[g.program.Fset.Position(fun.Decl.Name.Pos())], fun.Decl.Doc, fun.Doc)
		}
		for _, mth := range typ.Methods {
			g.emitDoc(objOf[g.program.Fset.Position(mth.Decl.Name.Pos())], mth.Decl.Doc, mth.Doc)
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

func (g *Grapher) emitDoc(obj types.Object, dc *ast.CommentGroup, docstring string) error {
	if obj == nil {
		return nil
	}
	if docstring == "" {
		return nil
	}

	if g.seenDocObjs == nil {
		g.seenDocObjs = make(map[types.Object]struct{})
	}
	if _, seen := g.seenDocObjs[obj]; seen {
		return fmt.Errorf("emitDoc: obj %v already seen", obj)
	}
	g.seenDocObjs[obj] = struct{}{}

	key, _, err := g.defInfo(obj)
	if err != nil {
		return err
	}

	if g.seenDocKeys == nil {
		g.seenDocKeys = make(map[string]struct{})
	}
	if _, seen := g.seenDocKeys[key.String()]; seen {
		return fmt.Errorf("emitDoc: key %v already seen", key)
	}
	g.seenDocKeys[key.String()] = struct{}{}

	var htmlBuf bytes.Buffer
	doc.ToHTML(&htmlBuf, docstring, nil)

	var filename string
	var span [2]int
	if dc != nil {
		filename = g.program.Fset.Position(dc.Pos()).Filename
		span = makeSpan(g.program.Fset, dc)
	}

	g.addDoc(&Doc{
		DefKey: key,
		Format: "text/html",
		Data:   htmlBuf.String(),
		File:   filename,
		Span:   span,
	})
	g.addDoc(&Doc{
		DefKey: key,
		Format: "text/plain",
		Data:   docstring,
		File:   filename,
		Span:   span,
	})

	return nil
}

func (g *Grapher) addDoc(doc *Doc) {
	g.Docs = append(g.Docs, doc)
}
