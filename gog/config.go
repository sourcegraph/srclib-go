package gog

import (
	"go/build"

	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types"
)

var Default = loader.Config{
	TypeChecker: types.Config{FakeImportC: true},
	Build:       &build.Default,
	AllowErrors: true,
}
