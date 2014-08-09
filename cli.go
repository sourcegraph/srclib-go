package main

import (
	"log"
	"os"

	"github.com/jessevdk/go-flags"
)

var (
	parser = flags.NewNamedParser("srclib-go", flags.Default)
	cwd    = getCWD()
)

func init() {
	parser.LongDescription = "srclib-go performs Go package, dependency, and source analysis."
}

func getCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return cwd
}

func main() {
	log.SetFlags(0)
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}
}
