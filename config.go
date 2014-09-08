package main

import (
	"encoding/json"
	"go/build"
	"log"
	"os"
	"path/filepath"
	"strings"

	"code.google.com/p/go.tools/go/loader"
	"code.google.com/p/go.tools/go/types"
)

var (
	buildContext = build.Default

	loaderConfig = loader.Config{
		TypeChecker: types.Config{FakeImportC: true},
		Build:       &buildContext,
		AllowErrors: true,
	}

	config *srcfileConfig

	// virtualCWD is the vfs cwd that corresponds to the non-vfs cwd, when using
	// vfs. It is used to determine whether a vfs path is effectively underneath
	// the cwd.
	virtualCWD string

	// dockerCWD is the original docker cwd before symlinking. If set (and if
	// running in Docker), it is used to determine whether a path is effectively
	// underneath the cwd.
	dockerCWD string
)

func init() {
	if buildContext.GOPATH == "" {
		log.Fatal("GOPATH must be set.")
	}
}

type srcfileConfig struct {
	GOROOT        string
	PkgPatterns   []string // pattern passed to `go list` (defaults to {"./..."})
	SourceImports bool
}

// unmarshalTypedConfig parses config from the Config field of the source unit.
// It stores it in the config global variable.
//
// Callers should typically call config.apply() after calling
// unmarshalTypedConfig to actually apply the config.
func unmarshalTypedConfig(cfg map[string]interface{}) error {
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
	if config.GOROOT != "" {
		// clean/absolutize all paths
		config.GOROOT = filepath.Clean(config.GOROOT)
		if !filepath.IsAbs(config.GOROOT) {
			config.GOROOT = filepath.Join(cwd, config.GOROOT)
		}

		buildContext.GOROOT = c.GOROOT
		loaderConfig.Build = &buildContext
	}

	loaderConfig.SourceImports = config.SourceImports

	return nil
}

func (c *srcfileConfig) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"GOARCH=" + buildContext.GOARCH,
		"GOOS=" + buildContext.GOOS,
		"GOROOT=" + buildContext.GOROOT,
		"GOPATH=" + buildContext.GOPATH,
	}
}

func pathHasPrefix(path, prefix string) bool {
	return prefix == "." || path == prefix || strings.HasPrefix(path, prefix+"/")
}
