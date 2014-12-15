package src

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"sourcegraph.com/sourcegraph/rwvfs"
	"sourcegraph.com/sourcegraph/srclib/buildstore"
	"sourcegraph.com/sourcegraph/srclib/config"
	"sourcegraph.com/sourcegraph/srclib/plan"
	"sourcegraph.com/sourcegraph/srclib/toolchain"

	"sourcegraph.com/sourcegraph/srclib/unit"
)

func init() {
	c, err := CLI.AddCommand("config",
		"reads & scans for project configuration",
		`Produces a configuration file suitable for building the repository or directory tree rooted at DIR (or the current directory if not specified).

The steps are:

1. Read user srclib config (SRCLIBPATH/.srclibconfig), if present.

2. Read configuration from the current directory's Srcfile (if present).

3. Scan for source units in the directory tree rooted at the current directory (or the root of the repository containing the current directory), using the scanners specified in either the user srclib config or the Srcfile (or otherwise the defaults).

The default values for --repo and --subdir are determined by detecting the current repository and reading its Srcfile config (if any).
`,
		&configCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
	c.Aliases = []string{"c"}

	SetRepoOptDefaults(c)
}

// getInitialConfig gets the initial config (i.e., the config that comes solely
// from the Srcfile, if any, and the external user config, before running the
// scanners).
func getInitialConfig(opt config.Options, dir Directory) (*config.Repository, error) {
	if dir != "" && dir != "." {
		log.Fatalf("Currently, only configuring the current directory tree is supported (i.e., no DIR argument). You provided %q.\n\nTo configure that directory, `cd %s` in your shell and rerun this command.", dir, dir)
	}

	if opt.Subdir != "." {
		// TODO(sqs): if we have overridden a repo, then we specify the
		// overridden config from the root dir of the repo. so, if you try to
		// configure from a subdir in an overridden repo, the config will be
		// wrong. disable this for now.
		log.Fatalf("Configuration is currently only supported at the root (top-level directory) of a repository, not in a subdirectory (%q).", opt.Subdir)
	}

	cfg, err := config.ReadRepository(string(dir), opt.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to read repository at %s: %s", dir, err)
	}

	if cfg.Scanners == nil {
		cfg.Scanners = config.SrclibPathConfig.Scanners
	}

	return cfg, nil
}

type ConfigCmd struct {
	config.Options

	ToolchainExecOpt `group:"execution"`
	BuildCacheOpt    `group:"build cache"`

	Output struct {
		Output string `short:"o" long:"output" description:"output format" default:"text" value-name:"text|json"`
	} `group:"output"`

	Args struct {
		Dir Directory `name:"DIR" default:"." description:"root directory of tree to configure"`
	} `positional-args:"yes"`

	w io.Writer // output stream to print to (defaults to os.Stdout)
}

var configCmd ConfigCmd

func (c *ConfigCmd) Execute(args []string) error {
	if c.w == nil {
		c.w = os.Stdout
	}

	if c.Args.Dir == "" {
		c.Args.Dir = "."
	}

	cfg, err := getInitialConfig(c.Options, c.Args.Dir)
	if err != nil {
		return err
	}

	if len(cfg.PreConfigCommands) > 0 {
		if err := runPreConfigCommands(string(c.Args.Dir), cfg.PreConfigCommands, c.ToolchainExecOpt); err != nil {
			return fmt.Errorf("PreConfigCommands: %s", err)
		}
	}

	if err := scanUnitsIntoConfig(cfg, c.Options, c.ToolchainExecOpt); err != nil {
		return fmt.Errorf("failed to scan for source units: %s", err)
	}

	currentRepo, err := OpenRepo(string(c.Args.Dir))
	if err != nil {
		return fmt.Errorf("failed to open repo: %s", err)
	}
	buildStore, err := buildstore.NewRepositoryStore(currentRepo.RootDir)
	if err != nil {
		return err
	}

	// Write source units to build cache.
	//
	// TODO(sqs): create Makefile.config that makes it more standard to recreate
	// these when the source unit defns change (and to determine when the source
	// unit defns change), with targets like:
	//
	// UNITNAME/UNITTYPE.unit.v0.json: setup.py mylib/foo.py
	//   src config --unit=UNITNAME@UNITTYPE
	//
	// or maybe a custom stale checker is better than just using file mtimes for
	// all the files (maybe just use setup.py as a prereq? but then how will we
	// update SourceUnit.Files list? SourceUnit.Globs could help here...)
	if !c.NoCacheWrite {
		if err := rwvfs.MkdirAll(buildStore, buildStore.CommitPath(currentRepo.CommitID)); err != nil {
			return err
		}
		for _, u := range cfg.SourceUnits {
			filename := buildStore.FilePath(currentRepo.CommitID, plan.SourceUnitDataFilename(unit.SourceUnit{}, u))
			if err := rwvfs.MkdirAll(buildStore, filepath.Dir(filename)); err != nil {
				return err
			}
			f, err := buildStore.Create(filename)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(f).Encode(u); err != nil {
				return err
			}
			if err := f.Close(); err != nil {
				log.Fatal(err)
			}
		}
	}

	if c.Output.Output == "json" {
		PrintJSON(cfg, "")
	} else {
		fmt.Fprintf(c.w, "SCANNERS (%d)\n", len(cfg.Scanners))
		for _, s := range cfg.Scanners {
			fmt.Fprintf(c.w, " - %s\n", s)
		}
		fmt.Fprintln(c.w)

		fmt.Fprintf(c.w, "SOURCE UNITS (%d)\n", len(cfg.SourceUnits))
		for _, u := range cfg.SourceUnits {
			fmt.Fprintf(c.w, " - %s: %s\n", u.Type, u.Name)
		}
		fmt.Fprintln(c.w)

		fmt.Fprintf(c.w, "CONFIG PROPERTIES (%d)\n", len(cfg.Config))
		for _, kv := range sortedMap(cfg.Config) {
			fmt.Fprintf(c.w, " - %s: %s\n", kv[0], kv[1])
		}
	}

	return nil
}

func sortedMap(m map[string]interface{}) [][2]interface{} {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)

	sorted := make([][2]interface{}, len(keys))
	for i, k := range keys {
		sorted[i] = [2]interface{}{k, m[k]}
	}
	return sorted
}

func runPreConfigCommands(dir string, cmds []string, execOpt ToolchainExecOpt) error {
	if mode := execOpt.ToolchainMode(); mode&toolchain.AsProgram > 0 {
		for _, cmdStr := range cmds {
			cmd := exec.Command("sh", "-c", cmdStr)
			cmd.Dir = dir
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("command %q: %s", cmdStr, err)
			}
		}
	} else if mode&toolchain.AsDockerContainer > 0 {
		// Build image
		dockerfile := []byte(`
FROM ubuntu:14.04
RUN apt-get update -qq && echo 2014-08-10
RUN apt-get install -qq curl git mercurial build-essential
RUN useradd -ms /bin/bash srclib
RUN mkdir /src
RUN chown srclib /src
USER srclib
WORKDIR /src
`)
		tmpdir, err := ioutil.TempDir("", "src-docker-preconfig")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpdir)

		if err := ioutil.WriteFile(filepath.Join(tmpdir, "Dockerfile"), dockerfile, 0600); err != nil {
			return err
		}

		// TODO(sqs): use a unique container ID

		const containerName = "src-preconfigcommands"
		buildCmd := exec.Command("docker", "build", "-t", containerName, ".")
		buildCmd.Dir = tmpdir
		buildCmd.Stdout, buildCmd.Stderr = os.Stderr, os.Stderr
		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("building PreConfigCommands Docker container: %s", err)
		}

		dir, err := filepath.Abs(dir)
		if err != nil {
			return err
		}

		// HACK: make the files writable by the docker user
		if err := exec.Command("chmod", "-R", "777", dir).Run(); err != nil {
			return fmt.Errorf("chmod -R 777 dir failed: %s", err)
		}

		for _, cmdStr := range cmds {
			cmd := exec.Command("docker", "run", "-v", dir+":/src", "--rm", "--entrypoint=/bin/bash", containerName)
			cmd.Args = append(cmd.Args, "-c", cmdStr)
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			log.Printf("Running PreConfigCommands Docker container: %v", cmd.Args)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("command %q: %s", cmdStr, err)
			}
		}
	} else {
		log.Fatalf("Can't run PreConfigCommands: unknown execution mode %q.", mode)
	}

	return nil
}
