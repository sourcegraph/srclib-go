package main

import (
	"encoding/json"
	"go/build"
	"go/importer"
	"go/parser"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go/types"

	"golang.org/x/tools/go/loader"
)

var (
	buildContext = build.Default

	loaderConfig = loader.Config{
		ParserMode: parser.ParseComments,
		TypeChecker: types.Config{
			Importer:    importer.Default(),
			FakeImportC: true,
		},
		Build:       &buildContext,
		AllowErrors: true,
	}

	config *srcfileConfig

	// effectiveConfigGOPATHs is a list of GOPATH dirs that were
	// created as a result of the GOPATH config property. These are
	// the dirs that are appended to the actual build context GOPATH.
	effectiveConfigGOPATHs []string
)

// isInEffectiveConfigGOPATH is true if dir is underneath any of the
// dirs in effectiveConfigGOPATHs.
func isInEffectiveConfigGOPATH(dir string) bool {
	for _, gopath := range effectiveConfigGOPATHs {
		if pathHasPrefix(dir, gopath) {
			return true
		}
	}
	return false
}

type srcfileConfig struct {
}

// unmarshalTypedConfig parses config from the Config field of the source unit.
// It stores it in the config global variable.
//
// Callers should typically call config.apply() after calling
// unmarshalTypedConfig to actually apply the config.
func unmarshalTypedConfig(cfg map[string]string) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	if config == nil {
		config = &srcfileConfig{}
	}

	return config.apply()
}

// apply applies the configuration.
func (c *srcfileConfig) apply() error {
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
