package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types"
)

var (
	buildContext = build.Default

	loaderConfig = loader.Config{
		TypeChecker: types.Config{FakeImportC: true},
		Build:       &buildContext,
		AllowErrors: true,
	}

	config *srcfileConfig

	goBinaryName string

	validVersions = []string{"", "1.3", "1.2", "1.1", "1"}

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

	// GOROOTForCmd, if set, is used as the GOROOT env var when
	// invoking the "go" tool.
	GOROOTForCmd string

	// GOPATH's colon-separated dirs, if specified, are made absolute
	// (prefixed with the directory that the repository being built is
	// checked out to) and the resulting value is appended to the
	// GOPATH environment variable during the build.
	GOPATH string

	// GOVERSION is the version of the go tool that srclib-go
	// should shell out to. If GOVERSION is empty, the system's
	// default go binary is used. The only valid values for
	// GOVERSION are the empty string, "1.3", "1.2", "1.1", and
	// "1", which are transformed into "go", "go1.3", ..., "go1",
	// respectively, when the binary is called.
	GOVERSION string

	PkgPatterns []string // pattern passed to `go list` (defaults to {"./..."})

	// SkipGodeps makes srclib-go skip the Godeps/_workspace directory when
	// scanning for packages. This causes references to those packages to point
	// to the files in their respective repositories instead of the local copies
	// inside of Godeps/_workspace.
	SkipGodeps bool

	// ImportPathRoot is a prefix which is used when converting from repository
	// URI to Go import path.
	ImportPathRoot string

	// VendorDirs are a list of all src directories used by a package,
	// relative to the repository root. These are used for
	// GO15VENDOREXPERIMENT related vendor support, and this is usually
	// not set by the user. At build time a GOPATH is created which will
	// include each VendorDir on it linked to src. VendorDirs should be
	// sorted by longest path to shortest (ie most specific to least
	// specific)
	VendorDirs []string
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
	// KLUDGE: determine whether we're in the stdlib and if so, set GOROOT to "." before applying config.
	// This is necessary for the stdlib unit names to be correct.
	cloneURL, _ := exec.Command("git", "config", "--get", "remote.origin.url").CombinedOutput()
	if strings.HasSuffix(strings.TrimSpace(string(cloneURL)), "github.com/golang/go") && c.GOROOT == "" {
		c.GOROOT = "."
	}

	var versionValid bool
	for _, v := range validVersions {
		if config.GOVERSION == v {
			versionValid = true
			goBinaryName = fmt.Sprintf("go%s", config.GOVERSION)
			if config.GOVERSION != "" && config.GOROOT == "" {
				// If GOROOT is empty, assign $GOROOT<version_num> to it.
				newGOROOT := os.Getenv(fmt.Sprintf("GOROOT%s", strings.Replace(config.GOVERSION, ".", "", -1)))
				if newGOROOT != "" {
					config.GOROOTForCmd = newGOROOT
				}
			}
			break
		}
	}
	if !versionValid {
		return fmt.Errorf("The version %s is not valid. Use one of the following: %v", config.GOVERSION, validVersions)
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

	config.VendorDirs = cleanDirs(config.VendorDirs)

	if config.GOROOTForCmd == "" {
		config.GOROOTForCmd = buildContext.GOROOT
	}

	return nil
}

func (c *srcfileConfig) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"GOARCH=" + buildContext.GOARCH,
		"GOOS=" + buildContext.GOOS,
		"GOROOT=" + config.GOROOTForCmd,
		"GOPATH=" + buildContext.GOPATH,
		"GO15VENDOREXPERIMENT=1",
	}
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
