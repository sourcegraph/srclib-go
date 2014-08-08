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
		Defs: make([]*graph.Def, len(o.Defs)),
		Refs: make([]*graph.Ref, len(o.Refs)),
		Docs: make([]*graph.Doc, len(o.Docs)),
	}

	uri := string(unit.Repo)

	for i, gs := range o.Defs {
		o2.Defs[i], err = convertGoDef(gs, uri)
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

// DefData is extra Go-specific data about a def.
type DefData struct {
	gog.DefInfo

	// PackageImportPath is the import path of the package containing this
	// def (if this def is not a package). If this def is a package,
	// PackageImportPath is its own import path.
	PackageImportPath string `json:",omitempty"`
}

func convertGoDef(gs *gog.Def, repoURI string) (*graph.Def, error) {
	resolvedTarget, err := ResolveDep(gs.DefKey.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	path := graph.DefPath(pathOrDot(strings.Join(gs.Path, "/")))
	treePath := treePath(string(path))
	if !treePath.IsValid() {
		return nil, fmt.Errorf("'%s' is not a valid tree-path", treePath)
	}

	def := &graph.Def{
		DefKey: graph.DefKey{
			Unit:     resolvedTarget.ToUnit,
			UnitType: resolvedTarget.ToUnitType,
			Path:     path,
		},
		TreePath: treePath,

		Name: gs.Name,
		Kind: graph.DefKind(gog.GeneralKindMap[gs.Kind]),

		File:     gs.File,
		DefStart: gs.DeclSpan[0],
		DefEnd:   gs.DeclSpan[1],

		Exported: gs.DefInfo.Exported,
		Test:     strings.HasSuffix(gs.File, "_test.go"),
	}

	d := DefData{
		PackageImportPath: gs.DefKey.PackageImportPath,
		DefInfo:           gs.DefInfo,
	}
	def.Data, err = json.Marshal(d)
	if err != nil {
		return nil, err
	}

	if def.Kind == "func" {
		def.Callable = true
	}

	return def, nil
}

func convertGoRef(gr *gog.Ref, repoURI string) (*graph.Ref, error) {
	resolvedTarget, err := ResolveDep(gr.Def.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	if resolvedTarget == nil {
		return nil, nil
	}

	return &graph.Ref{
		DefRepo:     uriOrEmpty(resolvedTarget.ToRepoCloneURL),
		DefPath:     graph.DefPath(pathOrDot(strings.Join(gr.Def.Path, "/"))),
		DefUnit:     resolvedTarget.ToUnit,
		DefUnitType: resolvedTarget.ToUnitType,
		Def:         gr.IsDef,
		File:        gr.File,
		Start:       gr.Span[0],
		End:         gr.Span[1],
	}, nil
}

func convertGoDoc(gd *gog.Doc, repoURI string) (*graph.Doc, error) {
	resolvedTarget, err := ResolveDep(gd.PackageImportPath, repoURI)
	if err != nil {
		return nil, err
	}
	return &graph.Doc{
		DefKey: graph.DefKey{
			Path:     graph.DefPath(pathOrDot(strings.Join(gd.Path, "/"))),
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
