package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"strings"

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

	output, err := gog.Main(config, flag.Args())
	if err != nil {
		log.Fatal(err)
	}

	err = json.NewEncoder(os.Stdout).Encode(output)
	if err != nil {
		log.Fatal(err)
	}
}
