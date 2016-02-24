package gog

import (
	"go/build"

	"go/types"

	"golang.org/x/tools/go/loader"
)

var Default = loader.Config{
	TypeChecker: types.Config{FakeImportC: true},
	Build:       &build.Default,
	AllowErrors: true,
}
