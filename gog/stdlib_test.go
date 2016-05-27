package gog

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/loader"
)

var stdlibPath = flag.String("test.stdlib-pkg", "", "in TestStdlib, only graph this package (import path)")

// adapted from go/types stdlib_test.go

var (
	pkgCount         int // number of packages processed
	defCount         int
	refCount         int
	unresolvedIdents int
	start            time.Time
)

func TestStdlib(t *testing.T) {
	if v, _ := strconv.ParseBool(os.Getenv("TEST_STDLIB")); !v {
		t.Skip()
	}

	if data, err := exec.Command("go", "list", "std").Output(); err == nil {
		lines := bytes.Split(data, []byte{'\n'})
		start = time.Now()
		for _, line := range lines {
			if path := string(line); path != "" && !strings.HasPrefix(path, "cmd/") {
				if *stdlibPath == "" || path == *stdlibPath {
					testPkg(t, path)
				}
			}
		}
	} else {
		t.Fatal(err)
	}

	if testing.Verbose() {
		fmt.Println(pkgCount, "packages graphed in", time.Since(start))
		fmt.Printf("totals: %d defs, %d refs\n", defCount, refCount)
		if unresolvedIdents > 0 {
			t.Logf("unresolved idents: %d", unresolvedIdents)
		}
	}
}

func testPkg(t *testing.T, path string) {
	if path == "unsafe" {
		return
	}
	conf := Default
	conf.Import(path)
	prog, err := conf.Load()
	if err != nil {
		t.Fatal(path, err)
	}

	start := time.Now()
	var output Output
	for _, pkgInfo := range prog.AllPackages {
		o := Graph(prog.Fset, pkgInfo.Files, pkgInfo.Pkg, &pkgInfo.Info, true)
		output.Append(o)

	}
	if testing.Verbose() {
		fmt.Printf("graphed %-22s\t% 4d msec   [% 6d defs, % 6d refs]\n", path, time.Since(start)/time.Millisecond, len(output.Defs), len(output.Refs))
	}
	pkgCount++
	defCount += len(output.Defs)
	refCount += len(output.Refs)

	checkAllIdents(t, &output, prog)
	checkUnique(t, &output, prog)
}

func checkUnique(t *testing.T, output *Output, prog *loader.Program) {
	defs := make(map[defPath]*Def, len(output.Defs))
	for _, s := range output.Defs {
		key := s.DefKey.defPath()
		if x, present := defs[key]; present {
			t.Errorf("def %+v %s:%d-%d already defined at %s:%d-%d", key, s.File, s.IdentSpan[0], s.IdentSpan[1], x.File, x.IdentSpan[0], x.IdentSpan[1])
		} else {
			defs[key] = s
		}
	}
}
