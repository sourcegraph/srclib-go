package golang

import (
	"encoding/json"
	"go/build"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"sourcegraph.com/sourcegraph/srclib/toolchain"
	"sourcegraph.com/sourcegraph/srclib/unit"
)

func Scan(pkgPattern string) ([]*unit.SourceUnit, error) {
	cmd := exec.Command("go", "list", "-e", "-json", pkgPattern)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	dec := json.NewDecoder(stdout)
	var units []*unit.SourceUnit
	for {
		var pkg *build.Package
		if err := dec.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		pv, pt := reflect.ValueOf(pkg).Elem(), reflect.TypeOf(*pkg)

		// collect all files
		var files []string
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if strings.HasSuffix(f.Name, "Files") {
				fv := pv.Field(i).Interface()
				files = append(files, fv.([]string)...)
			}
		}

		// collect all imports
		depsMap := map[string]struct{}{}
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if strings.HasSuffix(f.Name, "Imports") {
				fv := pv.Field(i).Interface()
				imports := fv.([]string)
				for _, imp := range imports {
					depsMap[imp] = struct{}{}
				}
			}
		}
		deps0 := make([]string, len(depsMap))
		i := 0
		for imp := range depsMap {
			deps0[i] = imp
			i++
		}
		sort.Strings(deps0)
		deps := make([]interface{}, len(deps0))
		for i, imp := range deps0 {
			deps[i] = imp
		}

		// make all dirs relative to the current dir
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if strings.HasSuffix(f.Name, "Dir") {
				fv := pv.Field(i)
				dir := fv.Interface().(string)
				if dir != "" {
					dir, err := filepath.Rel(cwd, dir)
					if err != nil {
						return nil, err
					}
					fv.Set(reflect.ValueOf(dir))
				}
			}
		}

		units = append(units, &unit.SourceUnit{
			Name:         pkg.ImportPath,
			Type:         "GoPackage",
			Files:        files,
			Data:         pkg,
			Dependencies: deps,
			Ops:          map[string]*toolchain.ToolRef{"depresolve": nil, "graph": nil},
		})
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return units, nil
}

var cwd string

func init() {
	var err error
	cwd, err = os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
}
