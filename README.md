# srclib-go [![Build Status](https://travis-ci.org/sourcegraph/srclib-go.png?branch=master)](https://travis-ci.org/sourcegraph/srclib-go)

**srclib-go** is a [srclib](https://sourcegraph.com/sourcegraph/srclib)
toolchain that performs Go code analysis: type checking, documentation
generation, jump-to-definition, dependency resolution, etc.

It enables this functionality in any client application whose code analysis is
powered by srclib, including:

* [emacs-sourcegraph-mode](https://sourcegraph.com/sourcegraph/emacs-sourcegraph-mode),
  an editor plugin for Emacs
* [Sourcegraph.com](https://sourcegraph.com), an open-source code search engine

Screenshots are below.

## Installation

This toolchain is not a standalone program; it provides additional functionality
to editor plugins and other applications that use [srclib](https://srclib.org).

First,
[install the `src` program (see srclib installation instructions)](https://sourcegraph.com/sourcegraph/srclib).

Then run:

```
# download and fetch dependencies
go get -v sourcegraph.com/sourcegraph/srclib-go
cd $GOPATH/sourcegraph.com/sourcegraph/srclib-go

# build the srclib-go program in .bin/srclib-go (this is currently required by srclib to discover the program)
make

# link this toolchain in your SRCLIBPATH (default ~/.srclib) to enable it
src toolchain add sourcegraph.com/sourcegraph/srclib-go
```

To verify that installation succeeded, run:

```
src toolchain list
```

You should see this srclib-go toolchain in the list.

Now that this toolchain is installed, any program that relies on srclib (such as
editor plugins) will support Go.

(TODO(sqs): add a tutorial link)

## Screenshot

Here's what srclib-go's analysis looks like in these applications.

The first screenshot shows the
[http.NewRequest function](https://sourcegraph.com/code.google.com/p/go/.GoPackage/net/http/.def/NewRequest)
on [Sourcegraph.com](https://sourcegraph.com). Here, srclib-go enables
clickable links for every identifier (that take you to their definitions),
automatic cross-repository usage examples, type inference, and documentation
generation.

![screenshot](https://s3-us-west-2.amazonaws.com/sourcegraph-assets/sourcegraph-go-screenshot-0.png "Sourcegraph.com Go screenshot")

The second screenshot shows the
[emacs-sourcegraph-mode plugin for Emacs](https://sourcegraph.com/sourcegraph/emacs-sourcegraph-mode)
with this toolchain installed. Here, srclib-go enables
jump-to-definition, type inference, documentation generation, and automatic
cross-repository usage examples from [Sourcegraph.com](https://sourcegraph.com).
All code analysis is performed locally by [srclib](https://srclib.org) using
this toolchain.

![screenshot](https://s3-us-west-2.amazonaws.com/sourcegraph-assets/emacs-sourcegraph-mode-screenshot-1.png "Emacs Go screenshot")

## Srcfile configuration

Go repositories built with this toolchain may specify the following
properties in their Srcfile's `Config` property:

* `"GoBaseImportPath:DIR": "IMPORT-PATH-PREFIX"`: this tells srclib-go to treat
  the directory tree DIR (relative to the Srcfile's directory) as being the root
  of the given import path prefix. This is used when your repository's import
  path doesn't correspond to its clone URL, or when you have a nonstandard
  repository layout.

  Both DIR and IMPORT-PATH-PREFIX can be `.` or any relative path, such as
  `a/b/c`.

  If no GoBaseImportPath is specified for a package's tree, its import path is
  constructed by looking at its position in the GOPATH (for the program
  execution method) or its repository's clone URL (for the Docker execution
  method). If multiple overlapping GoBaseImportPaths are specified, the behavior
  is undefined.

  For example, specifying `"GoBaseImportPath:.": "example.com/foo"` would mean
  that the top-level package's import path is considered to be
  `example.com/foo`, and a package in the `bar` subdirectory would have an
  import path of `example.com/foo/bar`.

  The Go standard library is considered to have an import path prefix of `.`,
  even though they originate from the repository `code.google.com/p/go`
  subdirectory `src/pkg`. Therefore, when building the Go stdlib, srclib-go uses
  the following configuration: `"GoBaseImportPath:src/pkg": "."`.

## Known issues

srclib-go is alpha-quality software. It powers code analysis on
[Sourcegraph.com](https://sourcegraph.com) but has not been widely tested or
adapted for other use cases. It also has several limitations.

* Does not properly special-case analysis of the Go standard library when
  checked out as a repository (code.google.com/p/go). It should rewrite import
  paths to eliminate the `code.google.com/p/go/` prefix and resolve internal
  references to its own packages, not the GOROOT standard packages for the
  currently installed version of Go. The version of this toolchain running on
  [Sourcegraph.com](https://sourcegraph.com) handles this correctly, but the
  functionality hasn't been ported yet.
* In some cases, multiple `init` functions in the same package with local
  variables of the same name are emitted using the same definition path, which
  causes an error. E.g., `func init() { x := 3; _ = x }; func init() { x := 3; _ = x }`.


## Tests

Testing this toolchain requires that you have installed `src` from
[srclib](https://sourcegraph.com/sourcegraph/srclib) and that you have this
toolchain set up. See srclib documentation for more information.

To test this toolchain's output against the expected output, run:

```
# build the Docker container to run the tests in isolation
src toolchain build sourcegraph.com/sourcegraph/srclib-go

# run the tests
src test
```

By default, that command runs tests in an isolated Docker container. To run the
tests on your local machine, run `src test -m program`. See the srclib
documentation for more information about the differences between these two
execution methods.

NOTE: The test expectation files are based on the output of Go 1.3 `go list
-json`. This is different from Go 1.2, so the tests won't pass on Go 1.2 (but
the functionality seems fine).

## Contributing

Patches are welcomed via GitHub pull request! See
[CONTRIBUTING.md](./CONTRIBUTING.md) for more information.

srclib-go's type analysis is based on
[go/types](https://godoc.org/code.google.com/p/go.tools/go/types).
