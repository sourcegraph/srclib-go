package main

import (
	"encoding/json"
	"go/build"
	"go/importer"
	"go/parser"
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
	// GOROOT, if specified, is made absolute (prefixed with the
	// directory that the repository being built is checked out to)
	// and is set as the GOROOT environment variable.
	GOROOT string

	// GOPATH's colon-separated dirs, if specified, are made absolute
	// (prefixed with the directory that the repository being built is
	// checked out to) and the resulting value is appended to the
	// GOPATH environment variable during the build.
	GOPATH string
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
	if err == nil && (strings.HasSuffix(cloneURL, "github.com/golang/go") || strings.HasSuffix(cloneURL, "github.com/sgtest/minimal-go-stdlib")) && c.GOROOT == "" {
		c.GOROOT = "."
	}

	if config.GOROOT != "" {
		// clean/absolutize all paths
		config.GOROOT = filepath.Clean(config.GOROOT)
		if !filepath.IsAbs(config.GOROOT) {
			config.GOROOT = filepath.Join(cwd, config.GOROOT)
		}

		buildContext.GOROOT = c.GOROOT
		loaderConfig.Build = &buildContext
	}

	if config.GOPATH != "" {
		// clean/absolutize all paths
		dirs := cleanDirs(filepath.SplitList(config.GOPATH))
		config.GOPATH = strings.Join(dirs, string(filepath.ListSeparator))

		dirs = append(dirs, filepath.SplitList(buildContext.GOPATH)...)
		buildContext.GOPATH = strings.Join(uniq(dirs), string(filepath.ListSeparator))
		loaderConfig.Build = &buildContext
	}

	return nil
}

func pathHasPrefix(path, prefix string) bool {
	return prefix == "." || path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator))
}

// cleanDirs takes a list of paths cleans/abs them + removes duplicates
func cleanDirs(dirs []string) []string {
	dirs = uniq(dirs)
	for i, dir := range dirs {
		dir = filepath.Clean(dir)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		dirs[i] = dir
	}
	return dirs
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
