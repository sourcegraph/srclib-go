package gog

import (
	"flag"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/loader"
)

var identFile = flag.String("test.idents", "", "print out all idents in files whose name contains this substring")
var resolve = flag.Bool("test.resolve", false, "test that refs resolve to existing defs (DEPRECATED)")

func TestIdentCoverage(t *testing.T) {
	files, err := filepath.Glob("testdata/*.go")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)

	g, prog := graphPkgFromFiles(t, "testdata", files)

	checkAllIdents(t, g, prog)
}

func (s *DefKey) defPath() defPath {
	return defPath{s.PackageImportPath, strings.Join(s.Path, "/")}
}

// checkAllIdents checks that every *ast.Ident has a corresponding Def or
// Ref.
func checkAllIdents(t *testing.T, output *Output, prog *loader.Program) {
	defs := make(map[defPath]struct{}, len(output.Defs))
	byIdentPos := make(map[identPos]interface{}, len(output.Defs)+len(output.Refs))
	for _, s := range output.Defs {
		defs[s.DefKey.defPath()] = struct{}{}
		byIdentPos[identPos{s.File, s.IdentSpan[0], s.IdentSpan[1]}] = s
	}
	for _, r := range output.Refs {
		byIdentPos[identPos{r.File, r.Span[0], r.Span[1]}] = r
	}
	for _, pkg := range prog.Created {
		for _, f := range pkg.Files {
			printAll := *identFile != "" && strings.Contains(prog.Fset.Position(f.Name.Pos()).Filename, *identFile)
			checkIdents(t, prog.Fset, f, byIdentPos, defs, printAll)
			checkUnique(t, output, prog)
		}
	}
}

type defPath struct {
	pkg  string
	path string
}

type identPos struct {
	file       string
	start, end uint32
}

func checkIdents(t *testing.T, fset *token.FileSet, file *ast.File, idents map[identPos]interface{}, defs map[defPath]struct{}, printAll bool) {
	ast.Inspect(file, func(n ast.Node) bool {
		if x, ok := n.(*ast.Ident); ok && x.Name != "_" {
			pos, end := fset.Position(x.Pos()), fset.Position(x.End())
			if printAll {
				t.Logf("ident %q at %s:%d-%d", x.Name, pos.Filename, pos.Offset, end.Offset)
			}
			ip := identPos{pos.Filename, uint32(pos.Offset), uint32(end.Offset)}
			if obj, present := idents[ip]; !present {
				t.Errorf("unresolved ident %q at %s", x.Name, pos)
			} else if ref, ok := obj.(*Ref); ok {
				if printAll {
					t.Logf("ref to %+v from ident %q at %s:%d-%d", ref.Def, x.Name, pos.Filename, pos.Offset, end.Offset)
				}
				if *resolve {
					if _, resolved := defs[ref.Def.defPath()]; !resolved && !ignoreRef(ref.Def.defPath()) {
						t.Errorf("unresolved ref %q to %+v at %s", x.Name, ref.Def.defPath(), pos)
						unresolvedIdents++
					}
				}
			}
			return false
		}
		return true
	})
}

func ignoreRef(dp defPath) bool {
	return dp.pkg == "builtin" || dp.pkg == "unsafe"
}
