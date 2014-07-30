package golang

import (
	"encoding/json"
	"fmt"

	"strings"

	"sourcegraph.com/sourcegraph/srclib-go/gog"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/grapher"
	"sourcegraph.com/sourcegraph/srclib/repo"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

func Graph(unit *unit.SourceUnit) (*grapher.Output, error) {
	pkg, err := UnitDataAsBuildPackage(unit)
	if err != nil {
		return nil, err
	}

	o, err := gog.Main(&gog.Default, []string{pkg.ImportPath})
	if err != nil {
		return nil, err
	}

	o2 := grapher.Output{
		Symbols: make([]*graph.Symbol, len(o.Symbols)),
		Refs:    make([]*graph.Ref, len(o.Refs)),
		Docs:    make([]*graph.Doc, len(o.Docs)),
	}

	uri := string(unit.Repo)

	for i, gs := range o.Symbols {
		o2.Symbols[i], err = convertGoSymbol(gs, uri)
		if err != nil {
			return nil, err
		}
	}
	for i, gr := range o.Refs {
		o2.Refs[i], err = convertGoRef(gr, uri)
		if err != nil {
			return nil, err
		}
	}
	for i, gd := range o.Docs {
		o2.Docs[i], err = convertGoDoc(gd, uri)
		if err != nil {
			return nil, err
		}
	}

	return &o2, nil
}

// SymbolData is extra Go-specific data about a symbol.
type SymbolData struct {
	gog.SymbolInfo

	// PackageImportPath is the import path of the package containing this
	// symbol (if this symbol is not a package). If this symbol is a package,
	// PackageImportPath is its own import path.
	PackageImportPath string `json:",omitempty"`
}

func convertGoSymbol(gs *gog.Symbol, repoURI string) (*graph.Symbol, error) {
	resolvedTarget, err := ResolveDep(gs.SymbolKey.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	path := graph.SymbolPath(pathOrDot(strings.Join(gs.Path, "/")))
	treePath := treePath(string(path))
	if !treePath.IsValid() {
		return nil, fmt.Errorf("'%s' is not a valid tree-path", treePath)
	}

	sym := &graph.Symbol{
		SymbolKey: graph.SymbolKey{
			Unit:     resolvedTarget.ToUnit,
			UnitType: resolvedTarget.ToUnitType,
			Path:     path,
		},
		TreePath: treePath,

		Name: gs.Name,
		Kind: graph.SymbolKind(gog.GeneralKindMap[gs.Kind]),

		File:     gs.File,
		DefStart: gs.DeclSpan[0],
		DefEnd:   gs.DeclSpan[1],

		Exported: gs.SymbolInfo.Exported,
		Test:     strings.HasSuffix(gs.File, "_test.go"),
	}

	d := SymbolData{
		PackageImportPath: gs.SymbolKey.PackageImportPath,
		SymbolInfo:        gs.SymbolInfo,
	}
	sym.Data, err = json.Marshal(d)
	if err != nil {
		return nil, err
	}

	if sym.Kind == "func" {
		sym.Callable = true
	}

	return sym, nil
}

func convertGoRef(gr *gog.Ref, repoURI string) (*graph.Ref, error) {
	resolvedTarget, err := ResolveDep(gr.Symbol.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	if resolvedTarget == nil {
		return nil, nil
	}

	return &graph.Ref{
		SymbolRepo:     uriOrEmpty(resolvedTarget.ToRepoCloneURL),
		SymbolPath:     graph.SymbolPath(pathOrDot(strings.Join(gr.Symbol.Path, "/"))),
		SymbolUnit:     resolvedTarget.ToUnit,
		SymbolUnitType: resolvedTarget.ToUnitType,
		Def:            gr.Def,
		File:           gr.File,
		Start:          gr.Span[0],
		End:            gr.Span[1],
	}, nil
}

func convertGoDoc(gd *gog.Doc, repoURI string) (*graph.Doc, error) {
	resolvedTarget, err := ResolveDep(gd.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	return &graph.Doc{
		SymbolKey: graph.SymbolKey{
			Path:     graph.SymbolPath(pathOrDot(strings.Join(gd.Path, "/"))),
			Unit:     resolvedTarget.ToUnit,
			UnitType: resolvedTarget.ToUnitType,
		},
		Format: gd.Format,
		Data:   gd.Data,
		File:   gd.File,
		Start:  gd.Span[0],
		End:    gd.Span[1],
	}, nil
}

func uriOrEmpty(cloneURL string) repo.URI {
	if cloneURL == "" {
		return ""
	}
	return repo.MakeURI(cloneURL)
}

func pathOrDot(path string) string {
	if path == "" {
		return "."
	}
	return path
}

func treePath(path string) graph.TreePath {
	if path == "" || path == "." {
		return graph.TreePath(".")
	}
	return graph.TreePath(fmt.Sprintf("./%s", path))
}
