package main

import (
	"encoding/json"

	"sourcegraph.com/sourcegraph/srclib/graph"
	"sourcegraph.com/sourcegraph/srclib/graph2"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

func convertUnits(u0 []*unit.SourceUnit) ([]*graph2.Unit, error) {
	u1 := make([]*graph2.Unit, len(u0))
	for i, u := range u0 {
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

		newU := &graph2.Unit{
			UnitKey: key,
			Globs:   u.Globs,
			Files:   u.Files,
			Dir:     u.Dir,
			// RawDeps: TODO(beyang),
			// Deps: TODO(beyang),
			DerivedFrom: nil,
			Info:        info,
			Data:        dataBytes,
		}
		u1[i] = newU
	}
	return u1, nil
}

func convertGraphOutput(out0 *graph.Output) (*graph2.Output, error) {
	return nil, nil
}
