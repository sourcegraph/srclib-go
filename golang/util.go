package golang

import (
	"encoding/json"
	"go/build"

	"github.com/sourcegraph/srclib/unit"
)

func UnitDataAsBuildPackage(u *unit.SourceUnit) (*build.Package, error) {
	data, err := json.Marshal(u.Data)
	if err != nil {
		return nil, err
	}

	var pkg *build.Package
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}
