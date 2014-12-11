package src

import (
	"log"
	"os"
	"path/filepath"

	"sourcegraph.com/sourcegraph/makex"

	"strings"

	"sourcegraph.com/sourcegraph/srclib/buildstore"
	"sourcegraph.com/sourcegraph/srclib/config"
	"sourcegraph.com/sourcegraph/srclib/flagutil"
	"sourcegraph.com/sourcegraph/srclib/plan"
)

func init() {
	c, err := CLI.AddCommand("make",
		"plans and executes plan",
		`Generates a plan (in Makefile form, in memory) for analyzing the tree and executes the plan. `,
		&makeCmd,
	)
	if err != nil {
		log.Fatal(err)
	}

	SetRepoOptDefaults(c)
}

type MakeCmd struct {
	config.Options

	ToolchainExecOpt `group:"execution"`
	BuildCacheOpt    `group:"build cache"`

	DryRun bool `short:"n" long:"dry-run" description:"print what would be done and exit"`

	Dir Directory `short:"C" long:"directory" description:"change to DIR before doing anything" value-name:"DIR"`

	Args struct {
		Goals []string `name:"GOALS..." description:"Makefile targets to build (default: all)"`
	} `positional-args:"yes"`
}

var makeCmd MakeCmd

func (c *MakeCmd) Execute(args []string) error {
	if c.Dir != "" {
		if err := os.Chdir(string(c.Dir)); err != nil {
			return err
		}
	}

	mf, err := CreateMakefile(c.ToolchainExecOpt)
	if err != nil {
		return err
	}

	goals := c.Args.Goals
	if len(goals) == 0 {
		if defaultRule := mf.DefaultRule(); defaultRule != nil {
			goals = []string{defaultRule.Target()}
		}
	}

	mkConf := &makex.Default
	mk := mkConf.NewMaker(mf, goals...)

	if c.DryRun {
		return mk.DryRun(os.Stdout)
	}

	return mk.Run()
}

// CreateMakefile creates a Makefile to build a tree. The cwd should
// be the root of the tree you want to make (due to some probably
// unnecessary assumptions that CreateMaker makes).
func CreateMakefile(execOpt ToolchainExecOpt) (*makex.Makefile, error) {
	currentRepo, err := OpenRepo(".")
	if err != nil {
		return nil, err
	}
	buildStore, err := buildstore.LocalRepo(currentRepo.RootDir)
	if err != nil {
		return nil, err
	}

	treeConfig, err := config.ReadCached(buildStore.Commit(currentRepo.CommitID))
	if err != nil {
		return nil, err
	}
	if len(treeConfig.SourceUnits) == 0 {
		log.Println("No source unit files found. Did you mean to run `src config`? (This is not an error; it just means that src didn't find anything to build or analyze here.)")
	}

	toolchainExecOptArgs, err := flagutil.MarshalArgs(&execOpt)
	if err != nil {
		return nil, err
	}

	// TODO(sqs): buildDataDir is hardcoded.
	buildDataDir := filepath.Join(buildstore.BuildDataDirName, currentRepo.CommitID)

	mf, err := plan.CreateMakefile(buildDataDir, treeConfig, plan.Options{ToolchainExecOpt: strings.Join(toolchainExecOptArgs, " ")})
	if err != nil {
		return nil, err
	}
	return mf, nil
}
