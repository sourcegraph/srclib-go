package gog

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveStructFields(t *testing.T) {
	cases := map[string]struct {
		pkgDefs   string
		localDefs string
		ref       string
		wantRefs  []*DefKey
	}{
		"basic struct field ref": {
			pkgDefs:   `type A struct {x string};`,
			localDefs: `var a A;`,
			ref:       `a.x`,
			wantRefs:  []*DefKey{{PackageImportPath: "foo", Path: []string{"A", "x"}}},
		},
		"multi-level named struct field ref": {
			pkgDefs:   `type A struct {a string};type B struct { a A };`,
			localDefs: `var b B;`,
			ref:       `b.a.a`,
			wantRefs: []*DefKey{
				{PackageImportPath: "foo", Path: []string{"A", "a"}},
				{PackageImportPath: "foo", Path: []string{"B", "a"}},
			},
		},
		"multi-level anon struct field ref": {
			pkgDefs:   `type A struct { B struct { c string } };`,
			localDefs: `var a A;`,
			ref:       `a.B.c`,
			wantRefs:  []*DefKey{{PackageImportPath: "foo", Path: []string{"A", "B", "c"}}},
		},
		"field in embedded struct ref": {
			pkgDefs:   `type A struct {a string};type B struct { A };`,
			localDefs: `var b B;`,
			ref:       `b.a`,
			wantRefs:  []*DefKey{{PackageImportPath: "foo", Path: []string{"A", "a"}}},
		},
		"embedded struct ref": {
			pkgDefs:   `type A struct {a string};type B struct { A };`,
			localDefs: `var b B;`,
			ref:       `b.A`,
			wantRefs:  []*DefKey{{PackageImportPath: "foo", Path: []string{"B", "A"}}},
		},

		"local: basic struct field ref": {
			pkgDefs:   ``,
			localDefs: `type A struct {x string}; var a A;`,
			ref:       `a.x`,
			wantRefs:  []*DefKey{{PackageImportPath: "foo", Path: []string{"_", "A", "x"}}},
		},

		"anonymous struct field ref": {
			ref:      `(struct{x int}{}).x`,
			wantRefs: []*DefKey{{PackageImportPath: "foo", Path: []string{"x$sources[0]47"}}},
		},

		"stdlib struct field ref": {
			pkgDefs:  `import "net/http";`,
			ref:      "http.DefaultClient.Transport",
			wantRefs: []*DefKey{{PackageImportPath: "net/http", Path: []string{"Client", "Transport"}}},
		},
		"stdlib method ref": {
			pkgDefs:  `import "net/http";`,
			ref:      "http.DefaultClient.CheckRedirect",
			wantRefs: []*DefKey{{PackageImportPath: "net/http", Path: []string{"Client", "CheckRedirect"}}},
		},
	}

	for label, c := range cases {
		src := `package foo; ` + c.pkgDefs + ` func _() { ` + c.localDefs + ` _ = /*START*/` + c.ref + `/*END*/; }`
		start, end := uint32(strings.Index(src, "/*START*/")), uint32(strings.Index(src, "/*END*/"))
		prog := createPkg(t, "foo", []string{src}, nil)

		pkgInfo := prog.Created[0]
		output := Graph(prog.Fset, pkgInfo.Files, pkgInfo.Pkg, &pkgInfo.Info, false)

		var refs []*Ref
		for _, r := range output.Refs {
			if r.Span[0] >= start && r.Span[1] <= end {
				refs = append(refs, r)
			}
		}

		var printAllRefs bool
		for _, wantRef := range c.wantRefs {
			var found bool
			for _, ref := range refs {
				if reflect.DeepEqual(ref.Def, wantRef) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: ref not found: %+v", label, wantRef)
				printAllRefs = true
			}
		}

		if printAllRefs {
			t.Logf("%s\n### Code:\n%s\n### All refs:", label, src)
			for _, ref := range refs {
				t.Logf("  %+v @ %s:%d-%d", ref.Def, ref.File, ref.Span[0], ref.Span[1])
			}
		}
	}
}
