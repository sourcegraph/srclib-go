package main

import (
	"log"
	"os"

	"github.com/jessevdk/go-flags"
)

var (
	parser = flags.NewNamedParser("srclib-go", flags.Default)
	cwd    string
)

func init() {
	parser.LongDescription = "srclib-go performs Go package, dependency, and source analysis."
}

func init() {
	var err error
	cwd, err = os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	log.SetFlags(0)
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}
}
