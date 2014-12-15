package makex

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	"code.google.com/p/rog-go/parallel"
)

// NewMaker creates a new Maker, which can build goals in a Makefile.
func (c *Config) NewMaker(mf *Makefile, goals ...string) *Maker {
	m := &Maker{
		mf:     mf,
		goals:  goals,
		Config: c,
	}
	m.buildDAG()
	return m
}

// A Maker can build goals in a Makefile.
type Maker struct {
	mf     *Makefile
	goals  []string
	dag    map[string][]string
	topo   [][]string
	cycles map[string][]string

	// RuleOutput specifies the writers to receive the stdout and stderr output
	// from executing a rule's recipes. After executing a rule, out and err are
	// closed. If RuleOutput is nil, os.Stdout and
	// os.Stderr are used, respectively (but not closed after use).
	RuleOutput func(Rule) (out io.WriteCloser, err io.WriteCloser, logger *log.Logger)

	// Channels to monitor progress. If non-nil, these channels are called at
	// various stages of building targets. Ended is always called *after*
	// Succeeded or Failed.
	Started, Ended, Succeeded chan<- Rule
	Failed                    chan<- RuleBuildError

	*Config
}

// buildDAG topologically sorts the targets based on their
// dependencies.
func (m *Maker) buildDAG() {
	// topological sort taken from
	// http://rosettacode.org/wiki/Topological_sort#Go.

	if m.dag == nil || m.cycles == nil {
		m.dag = make(map[string][]string)
		m.cycles = make(map[string][]string)
	}

	seen := make(map[string]struct{})
	queue := append([]string{}, m.goals...)
	for {
		if len(queue) == 0 {
			break
		}
		origLen := len(queue)
		for _, target := range queue {
			if _, seen := seen[target]; seen {
				continue
			}
			seen[target] = struct{}{}

			rule := m.mf.Rule(target)
			if rule == nil {
				continue
			}
			prereqs := uniqAndSort(rule.Prereqs())
			m.dag[target] = append([]string{}, prereqs...)
			for _, dep := range prereqs {
				// make a node for the prereq target even if it isn't defined
				queue = append(queue, dep)
				m.dag[dep] = m.dag[dep]
			}
		}
		queue = queue[origLen:]
	}

	// topological sort on the DAG
	for len(m.dag) > 0 {

		// collect targets with no dependencies
		var zero []string
		for target, prereqs := range m.dag {
			if len(prereqs) == 0 {
				zero = append(zero, target)
				delete(m.dag, target)
			}
		}

		// cycle detection
		if len(zero) == 0 {
			// collect un-orderable dependencies
			cycle := make(map[string]bool)
			for _, prereqs := range m.dag {
				for _, dep := range prereqs {
					cycle[dep] = true
				}
			}

			// mark targets with un-orderable dependencies
			for target, prereqs := range m.dag {
				if cycle[target] {
					m.cycles[target] = prereqs
				}
			}
			return
		}

		// output a set that can be processed concurrently
		m.topo = append(m.topo, zero)

		// remove edges (dependencies) from dg
		for _, remove := range zero {
			for target, prereqs := range m.dag {
				for i, dep := range prereqs {
					if dep == remove {
						copy(prereqs[i:], prereqs[i+1:])
						m.dag[target] = prereqs[:len(prereqs)-1]
						break
					}
				}
			}
		}
	}
}

// TargetSets returns a topologically sorted list of sets of target
// names. To only get targets that are stale and need to be built, use
// TargetSetsNeedingBuild.
func (m *Maker) TargetSets() [][]string {
	return m.topo
}

// TargetSetsNeedingBuild returns a topologically sorted list of sets
// of target names that need to be built (i.e., that are stale).
func (m *Maker) TargetSetsNeedingBuild() ([][]string, error) {
	for _, goal := range m.goals {
		if rule := m.mf.Rule(goal); rule == nil {
			return nil, errNoRuleToMakeTarget(goal)
		}
		if deps, isCycle := m.cycles[goal]; isCycle {
			return nil, errCircularDependency(goal, deps)
		}
	}

	targetSets := make([][]string, 0)
	for _, targetSet := range m.topo {
		var targetsNeedingBuild []string
		for _, target := range targetSet {
			exists, err := m.pathExists(target)
			if err != nil {
				return nil, err
			}
			if !exists {
				rule := m.mf.Rule(target)
				if rule == nil {
					return nil, errNoRuleToMakeTarget(target)
				}
				targetsNeedingBuild = append(targetsNeedingBuild, target)
			}
		}
		if len(targetsNeedingBuild) > 0 {
			targetSets = append(targetSets, targetsNeedingBuild)
		}
	}
	return targetSets, nil
}

// DryRun prints information about what targets *would* be built if Run() was
// called.
func (m *Maker) DryRun(w io.Writer) error {
	targetSets, err := m.TargetSetsNeedingBuild()
	if err != nil {
		return err
	}
	if len(targetSets) == 0 {
		fmt.Fprintln(w, "No target sets need building.")
	}
	for i, targetSet := range targetSets {
		if i != 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "========= TARGET SET %d (%d targets)\n", i, len(targetSet))
		for _, target := range targetSet {
			fmt.Fprintln(w, " - ", target)
		}
	}
	return nil
}

// ruleOutput determines the io.Writers to receive the stderr and stdout output
// of a rule's recipe commands.
func (m *Maker) ruleOutput(r Rule) (stdout io.WriteCloser, stderr io.WriteCloser, logger *log.Logger) {
	if m.RuleOutput != nil {
		return m.RuleOutput(r)
	}
	return nopCloser{os.Stdout}, nopCloser{os.Stderr}, log.New(os.Stderr, fmt.Sprintf("%s: ", r.Target()), 0)
}

// Run builds all stale targets.
func (m *Maker) Run() error {
	targetSets, err := m.TargetSetsNeedingBuild()
	if err != nil {
		return err
	}

	for _, targetSet := range targetSets {
		par := parallel.NewRun(m.ParallelJobs)
		for _, target := range targetSet {
			rule := m.mf.Rule(target)
			par.Do(func() error {
				stdout, stderr, log := m.ruleOutput(rule)
				if m.Started != nil {
					m.Started <- rule
				}
				defer stdout.Close()
				defer stderr.Close()
				defer func() {
					if m.Ended != nil {
						m.Ended <- rule
					}
				}()

				for _, recipe := range rule.Recipes() {
					recipe = ExpandAutoVars(rule, recipe)
					if m.Verbose {
						log.Printf("running command: %s", recipe)
					}
					cmd := exec.Command("sh", "-c", recipe)
					cmd.Stdout, cmd.Stderr = stdout, stderr

					err := cmd.Run()
					if err != nil {
						// remove files if failed
						if exists, _ := m.pathExists(rule.Target()); exists {
							err2 := m.fs().Remove(rule.Target())
							if err2 != nil {
								log.Printf("failed to remove target after error: %s", err)
							}
						}

						log.Printf(`command failed: %s (%s)`, recipe, err)
						err2 := RuleBuildError{rule, fmt.Errorf("command failed: %s (%s)", recipe, err)}
						if m.Failed != nil {
							m.Failed <- err2
						}
						return err2
					}
				}

				if m.Succeeded != nil {
					m.Succeeded <- rule
				}

				return nil
			})
		}
		err := par.Wait()
		if err != nil {
			return Errors(err.(parallel.Errors))
		}
	}

	return nil
}

type RuleBuildError struct {
	Rule Rule
	Err  error
}

func (e RuleBuildError) Error() string { return e.Err.Error() }

func errNoRuleToMakeTarget(target string) error {
	return fmt.Errorf("no rule to make target %q", target)
}

func errCircularDependency(target string, deps []string) error {
	return fmt.Errorf("circular dependency for target %q: %v", target, deps)
}

type nopCloser struct {
	io.Writer
}

func (nc nopCloser) Close() error { return nil }
