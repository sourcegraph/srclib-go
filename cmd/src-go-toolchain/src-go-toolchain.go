package main

import (
	"flag"
	"log"
	"runtime"
)

var version = "0.0.1"

func main() {
	log.SetFlags(0)
	flag.Parse()

	if flag.NArg() == 1 {
		switch flag.Arg(0) {
		case "version":
			log.Println(version)
		case "info":
			log.Printf("srclib go toolchain v%s - %s GOARCH=%s GOOS=%s", version, runtime.Version(), runtime.GOARCH, runtime.GOOS)
		}
	}
}
