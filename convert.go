package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"log"

	"strings"

	"sourcegraph.com/sourcegraph/srclib/dep"
	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/graph2"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

func convertUnits(u0 []*unit.SourceUnit) ([]*graph2.Unit, error) {
	u1 := make([]*graph2.Unit, len(u0))
	for i, u := range u0 {
		var err error
		u1[i], err = convertUnit(u)
		if err != nil {
			return nil, err
		}
	}
	return u1, nil
}

func convertUnit(u *unit.SourceUnit) (*graph2.Unit, error) {
	dataBytes, err := json.Marshal(u.Data)
	if err != nil {
		return nil, err
	}

	var key graph2.UnitKey
	key.TreeType = "git"
	key.URI = u.Repo
	key.Version = u.CommitID
	key.UnitName = u.Name
	key.UnitType = u.Type

	var info *graph2.UnitInfo
	if u.Info != nil {
		info = &graph2.UnitInfo{
			NameInRepository: u.Info.NameInRepository,
			GlobalName:       u.Info.GlobalName,
			Description:      u.Info.Description,
			TypeName:         u.Info.TypeName,
		}
	}

	var newDeps []*graph2.Dep
	for _, dep := range u.Dependencies {
		if newDep, err := convertRawDep(dep); err == nil {
			newDeps = append(newDeps, newDep)
		} else {
			log.Printf("warning: %s", err)
		}
	}

	return &graph2.Unit{
		UnitKey:     key,
		Globs:       u.Globs,
		Files:       u.Files,
		Dir:         u.Dir,
		Deps:        newDeps,
		DerivedFrom: nil,
		Info:        info,
		Data:        dataBytes,
	}, nil
}

func deconvertUnit(u *graph2.Unit) (*unit.SourceUnit, error) {
	var info *unit.Info
	if u.Info != nil {
		info = &unit.Info{
			NameInRepository: u.Info.NameInRepository,
			GlobalName:       u.Info.GlobalName,
			Description:      u.Info.Description,
			TypeName:         u.Info.TypeName,
		}
	}

	var data build.Package
	if err := json.Unmarshal(u.Data, &data); err != nil {
		return nil, err
	}

	var oldDeps []interface{}
	for _, dep := range u.Deps {
		oldDeps = append(oldDeps, deconvertRawDep(dep))
	}

	return &unit.SourceUnit{
		Name:         u.UnitName,
		Type:         u.UnitType,
		Repo:         u.URI,
		CommitID:     u.Version,
		Globs:        u.Globs,
		Files:        u.Files,
		Dir:          u.Dir,
		Dependencies: oldDeps,
		Info:         info,
		Data:         &data,
		// Config: TODO,
		// Ops: TODO,
	}, nil
}

func convertDefKey(d graph.DefKey) graph2.NodeKey {
	return graph2.NewNodeKey("git", d.Repo, d.CommitID, d.Unit, d.UnitType, fmt.Sprintf("def:%s", d.Path))
}

func convertRefKey(r *graph.Ref) graph2.NodeKey {
	return graph2.NewNodeKey("git", r.Repo, r.CommitID, r.Unit, r.UnitType, fmt.Sprintf("ref:%s:%d-%d:%s", r.File, r.Start, r.End, r.DefKey().Path))
}

func convertDocKey(d *graph.Doc) graph2.NodeKey {
	return graph2.NewNodeKey("git", d.Repo, d.CommitID, d.Unit, d.UnitType, fmt.Sprintf("doc:%s:%s:%d-%d:%s", d.Format, d.File, d.Start, d.End, d.Path))
}

func convertDef(d *graph.Def) *graph2.Node {
	return &graph2.Node{
		NodeKey: convertDefKey(d.DefKey),
		Kind:    "def",
		File:    d.File,
		Start:   d.DefStart,
		End:     d.DefEnd,
		Def: &graph2.DefData{
			Name:     d.Name,
			Kind:     d.Kind,
			Exported: d.Exported,
			Local:    d.Local,
			Test:     d.Test,
			Data:     []byte(d.Data),
		},
	}
}

func convertDoc(d *graph.Doc) (*graph2.Node, *graph2.Edge) {
	docKey := convertDocKey(d)
	return &graph2.Node{
			NodeKey: docKey,
			Kind:    "doc",
			File:    d.File,
			Start:   d.Start,
			End:     d.End,
			Doc: &graph2.DocData{
				Format: d.Format,
				Data:   d.Data,
			},
		}, &graph2.Edge{
			Src:  docKey,
			Dst:  convertDefKey(d.DefKey),
			Kind: "documents",
		}
}

func convertRef(r *graph.Ref) (*graph2.Node, *graph2.Edge) {
	refKey := convertRefKey(r)
	return &graph2.Node{
			NodeKey: refKey,
			Kind:    "ref",
			File:    r.File,
			Start:   r.Start,
			End:     r.End,
		}, &graph2.Edge{
			Src:  refKey,
			Dst:  convertDefKey(r.DefKey()),
			Kind: "ref",
		}
}

func convertGraphOutput(out0 *graph.Output) (*graph2.Output, error) {
	defNodes := make([]*graph2.Node, len(out0.Defs))
	refNodes, refEdges := make([]*graph2.Node, len(out0.Refs)), make([]*graph2.Edge, len(out0.Refs))
	docNodes, docEdges := make([]*graph2.Node, len(out0.Docs)), make([]*graph2.Edge, len(out0.Docs))
	for i, def := range out0.Defs {
		defNodes[i] = convertDef(def)
	}
	for i, ref := range out0.Refs {
		refNodes[i], refEdges[i] = convertRef(ref)
	}
	for i, doc := range out0.Docs {
		docNodes[i], docEdges[i] = convertDoc(doc)
	}

	return &graph2.Output{
		DefNodes: defNodes,
		RefNodes: refNodes,
		RefEdges: refEdges,
		DocNodes: docNodes,
		DocEdges: docEdges,
		Anns:     out0.Anns,
	}, nil
}

func convertRawDep(rd interface{}) (*graph2.Dep, error) {
	switch rd := rd.(type) {
	case string:
		return &graph2.Dep{Raw: graph2.RawDep{Name: rd}}, nil
	default:
		return nil, fmt.Errorf("could not convert dep of type %T: %+v", rd, rd)
	}
}

func deconvertRawDep(rd *graph2.Dep) interface{} {
	depStr := rd.Raw.Name
	if rd.Raw.Version != "" {
		depStr += "==" + rd.Raw.Version
	}
	return rd.Raw.Name
}

func convertDepOutput(deps []*dep.Resolution) ([]*graph2.Dep, error) {
	newDeps := make([]*graph2.Dep, len(deps))
	for i, dep := range deps {
		newDeps[i] = convertDep(dep)
	}
	return newDeps, nil
}

func convertDep(dep *dep.Resolution) *graph2.Dep {
	var rawDep string
	if depStringer, stringable := dep.Raw.(fmt.Stringer); stringable {
		rawDep = depStringer.String()
	} else {
		rawDep = fmt.Sprintf("%v", dep.Raw)
	}

	// TODO(beyang): robustly parse out the version from the rawDep string
	var rawVersion string
	if strings.Contains(rawDep, "==") {
		parts := strings.SplitN(rawDep, "==", 2)
		rawDep, rawVersion = parts[0], parts[1]
	}

	var resolvedDep *graph2.UnitKey
	if dep.Target != nil {
		resolvedDep = &graph2.UnitKey{
			TreeKey:  graph2.TreeKey{URI: dep.Target.ToRepoCloneURL, TreeType: "git"},
			Version:  dep.Target.ToVersionString,
			UnitName: dep.Target.ToUnit,
			UnitType: dep.Target.ToUnitType,
		}
	}
	return &graph2.Dep{
		Raw: graph2.RawDep{
			Name:    rawDep,
			Version: rawVersion,
		},
		Dep: resolvedDep,
		Err: dep.Error,
	}
}
