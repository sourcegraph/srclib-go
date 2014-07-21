package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"io"
	"log"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"

	"github.com/sourcegraph/srclib/unit"
)

var version = "0.0.1"

func init() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, prog+` performs Go package, dependency, and source analysis.

Usage:

        `+prog+` [options] command [arg...]

The commands are:
`)
		for _, c := range subcommands {
			fmt.Fprintf(os.Stderr, "    %-24s %s\n", c.Name, c.Description)
		}
		fmt.Fprintln(os.Stderr, `
Use "`+prog+` command -h" for more information about a command.

The options are:
`)
		flag.PrintDefaults()
		os.Exit(1)
	}
}

func main() {
	log.SetFlags(0)

	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
	}

	subcmd := flag.Arg(0)
	extraArgs := flag.Args()[1:]
	for _, c := range subcommands {
		if c.Name == subcmd {
			c.Run(extraArgs)
			return
		}
	}

	fmt.Fprintf(os.Stderr, prog+": unknown subcommand %q\n", subcmd)
	fmt.Fprintln(os.Stderr, `Run "`+prog+` -h" for usage.`)
	os.Exit(1)
}

var prog = os.Args[0]

type subcommand struct {
	Name, Description string
	Run               func(args []string)
}

var subcommands = []subcommand{
	{"version", "print version", func(args []string) { log.Println(version) }},
	{"info", "show info", func(args []string) {
		log.Printf("srclib go toolchain v%s - %s GOARCH=%s GOOS=%s", version, runtime.Version(), runtime.GOARCH, runtime.GOOS)
	}},
	{"scan", "discover Go packages", scanCmd},
}

func init() {
	// avoid init loop
	subcommands = append(subcommands, subcommand{"help", "show help info", helpCmd})
}

func scanCmd(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: `+prog+` scan [options] [dir]

Scans dir for source units. If dir is not given, the current directory is used.

The options are:
`)
		fs.PrintDefaults()
		os.Exit(1)
	}
	fs.Parse(args)

	var dir string
	switch fs.NArg() {
	case 0:
	case 1:
		dir = fs.Arg(0)
	default:
		fs.Usage()
	}

	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	dec := json.NewDecoder(stdout)
	var units []*unit.SourceUnit
	for {
		var pkg *build.Package
		if err := dec.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}

		// fix up import path (which currently begins with  "_/" because `go
		// list` could not locate the dir in the GOPATH
		pkg.ImportPath = "TODO" + strings.TrimPrefix(pkg.ImportPath, "_/")

		// collect all files
		var files []string
		pv, pt := reflect.ValueOf(*pkg), reflect.TypeOf(*pkg)
		for i := 0; i < pt.NumField(); i++ {
			f := pt.Field(i)
			if strings.HasSuffix(f.Name, "Files") {
				fv := pv.Field(i).Interface()
				files = append(files, fv.([]string)...)
			}
		}

		units = append(units, &unit.SourceUnit{
			Name:  pkg.ImportPath,
			Type:  "GoPackage",
			Files: files,
			Data:  pkg,
		})
	}
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(units); err != nil {
		log.Fatal(err)
	}
}

func helpCmd(args []string) {
	fs := flag.NewFlagSet("help", flag.ExitOnError)
	quiet := fs.Bool("q", false, "quiet (only show subcommand names)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: `+prog+` help [command]

Shows information about a `+prog+` command (if specified).

The options are:
`)
		fs.PrintDefaults()
		os.Exit(1)
	}
	fs.Parse(args)

	switch fs.NArg() {
	case 0:
		if !*quiet {
			flag.Usage()
		}
		for _, c := range subcommands {
			fmt.Println(c.Name)
		}

	case 1:
		subcmd := fs.Arg(0)
		for _, c := range subcommands {
			if c.Name == subcmd {
				c.Run([]string{"-h"})
				return
			}
		}

	default:
		fs.Usage()
	}
}
