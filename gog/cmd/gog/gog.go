package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"strings"

	"golang.org/x/tools/go/loader"

	"sourcegraph.com/sourcegraph/srclib-go/gog"
)

var buildTags = flag.String("tags", "", "a list of build tags to consider satisfied")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gog [options] [packages]\n\n")
		fmt.Fprintf(os.Stderr, "Graphs the named Go package.\n\n")
		fmt.Fprintf(os.Stderr, "The options are:\n")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "For more about specifying packages, see 'go help packages'.\n")
		os.Exit(1)
	}
	flag.Parse()

	log.SetFlags(0)

	config := &gog.Default

	if tags := strings.Split(*buildTags, ","); *buildTags != "" {
		build.Default.BuildTags = tags
		config.Build.BuildTags = tags
		log.Printf("Using build tags: %q", tags)
	}

	var importUnsafe bool
	for _, a := range flag.Args() {
		if a == "unsafe" {
			importUnsafe = true
			break
		}
	}

	extraArgs, err := config.FromArgs(flag.Args(), true)
	if err != nil {
		log.Fatal(err)
	}
	if len(extraArgs) > 0 {
		log.Fatal("extra args after pkgs list")
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
		log.Fatal(err)
	}

	g := gog.New(prog)

	var pkgs []*loader.PackageInfo
	pkgs = append(pkgs, prog.Created...)
	for _, pkg := range prog.Imported {
		pkgs = append(pkgs, pkg)
	}

	for _, pkg := range pkgs {
		if err := g.Graph(pkg.Files, pkg.Pkg, &pkg.Info); err != nil {
			log.Fatal(err)
		}
	}

	err = json.NewEncoder(os.Stdout).Encode(&g.Output)
	if err != nil {
		log.Fatal(err)
	}
}
