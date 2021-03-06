package main

import (
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	buildContext = build.Default
)

func initBuildContext() error {
	buildContext.InstallSuffix = "srclib"

	// KLUDGE: determine whether we're in the stdlib and if so, set GOROOT to "." before applying config.
	// This is necessary for the stdlib unit names to be correct.
	output, err := exec.Command("git", "config", "--get", "remote.origin.url").Output()
	cloneURL := strings.Replace(strings.TrimSuffix(strings.TrimSpace(string(output)), ".git"), ":", "/", -1)
	if err == nil && (strings.HasSuffix(cloneURL, "github.com/golang/go") || strings.HasSuffix(cloneURL, "github.com/sgtest/minimal-go-stdlib")) {
		buildContext.GOROOT = cwd
	}

	// Automatically detect vendored dirs (check for vendor/src and
	// Godeps/_workspace/src) and set up GOPATH pointing to them if
	// they exist.
	//
	// Note that the `vendor` directory here is used by 3rd party vendoring
	// tools and is NOT the `vendor` directory in the Go 1.6 official vendor
	// specification (that `vendor` directory does not have a `src`
	// subdirectory).
	var gopaths []string
	for _, vdir := range []string{"vendor", "Godeps/_workspace"} {
		if fi, err := os.Stat(filepath.Join(cwd, vdir, "src")); err == nil && fi.Mode().IsDir() {
			gopaths = append(gopaths, filepath.Join(cwd, vdir))
			log.Printf("Adding %s to GOPATH (auto-detected Go vendored dependencies source dir %s).", vdir, filepath.Join(vdir, "src"))
		}
	}
	gopaths = append(gopaths, filepath.SplitList(buildContext.GOPATH)...)
	buildContext.GOPATH = strings.Join(gopaths, string(filepath.ListSeparator))

	return nil
}

func pathHasPrefix(path, prefix string) bool {
	return prefix == "." || path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator))
}

// uniq maintains the order of s.
func uniq(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	var uniq []string
	for _, s := range s {
		if _, seen := seen[s]; seen {
			continue
		}
		seen[s] = struct{}{}
		uniq = append(uniq, s)
	}
	return uniq
}
