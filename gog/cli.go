package gog

import (
	"fmt"
	"log"

	"code.google.com/p/go.tools/go/loader"
)

// Main is like calling the 'gog' program.
func Main(config *loader.Config, args []string) (*Output, error) {
	var importUnsafe bool
	for _, a := range args {
		if a == "unsafe" {
			importUnsafe = true
			break
		}
	}

	extraArgs, err := config.FromArgs(args, true)
	if err != nil {
		log.Fatal(err)
	}
	if len(extraArgs) > 0 {
		return nil, fmt.Errorf("extra args after pkgs list")
	}

	if importUnsafe {
		// Special-case "unsafe" because go/loader does not let you load it
		// directly.
		if config.ImportPkgs == nil {
			config.ImportPkgs = make(map[string]bool)
		}
		config.ImportPkgs["unsafe"] = true
	}

	prog, err := config.Load()
	if err != nil {
		return nil, err
	}

	g := New(prog)

	var pkgs []*loader.PackageInfo
	pkgs = append(pkgs, prog.Created...)
	for _, pkg := range prog.Imported {
		pkgs = append(pkgs, pkg)
	}

	for _, pkg := range pkgs {
		if err := g.Graph(pkg); err != nil {
			return nil, err
		}
	}

	return &g.Output, nil
}
