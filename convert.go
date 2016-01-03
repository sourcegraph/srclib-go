package main

import (
	"encoding/json"
	"fmt"

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
	key.Genus = "git"
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

	return &graph2.Unit{
		UnitKey: key,
		Globs:   u.Globs,
		Files:   u.Files,
		Dir:     u.Dir,
		// RawDeps: TODO(beyang),
		// Deps: TODO(beyang),
		DerivedFrom: nil,
		Info:        info,
		Data:        dataBytes,
	}, nil
}

func convertDefKey(d graph.DefKey) graph2.NodeKey {
	return graph2.NewNodeKey("git", d.Repo, d.CommitID, d.Unit, d.UnitType, fmt.Sprintf("def:%s", d.Path))
}

func convertRefKey(r *graph.Ref) graph2.NodeKey {
	return graph2.NewNodeKey("git", r.Repo, r.CommitID, r.Unit, r.UnitType, fmt.Sprintf("ref:%s:%s-%s:%s", r.File, r.Start, r.End, r.DefKey().Path))
}

func convertDocKey(d graph.DefKey) graph2.NodeKey {
	return graph2.NewNodeKey("git", d.Repo, d.CommitID, d.Unit, d.UnitType, fmt.Sprintf("doc:%s", d.Path))
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
	docKey := convertDocKey(d.DefKey)
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
