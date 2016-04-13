# srclib-go [![Build Status](https://travis-ci.org/sourcegraph/srclib-go.svg?branch=master)](https://travis-ci.org/sourcegraph/srclib-go)

**srclib-go** is a [srclib](https://srclib.org)
toolchain that performs Go code analysis: type checking, documentation
generation, jump-to-definition, dependency resolution, etc.

It enables this functionality in any client application whose code analysis is
powered by srclib, including [Sourcegraph.com](https://sourcegraph.com).

Screenshots are below.

## Installation

This toolchain is not a standalone program; it provides additional functionality
to applications that use [srclib](https://srclib.org).

First,
[install the `srclib` program (see srclib installation instructions)](https://sourcegraph.com/sourcegraph/srclib).

Then run:

```
# download and fetch dependencies
go get -v sourcegraph.com/sourcegraph/srclib-go
cd $GOPATH/src/sourcegraph.com/sourcegraph/srclib-go

# build the srclib-go program in .bin/srclib-go (this is currently required by srclib to discover the program)
make

# link this toolchain in your SRCLIBPATH (default ~/.srclib) to enable it
```

To verify that installation succeeded, run:

```
srclib toolchain list
```

You should see this srclib-go toolchain in the list.

Now that this toolchain is installed, any program that relies on srclib will support Go.

(TODO(sqs): add a tutorial link)

## Screenshot

Here's what srclib-go's analysis looks like in these applications.

The first screenshot shows the
[http.NewRequest function](https://sourcegraph.com/github.com/golang/go/.GoPackage/net/http/.def/NewRequest)
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

## Usage

srclib-go only works with code that exists in a proper
[GOPATH](https://golang.org/doc/code.html#GOPATH). When you run the `src`
tool, it should use this GOPATH environment variable.

## Srcfile configuration

Go repositories built with this toolchain may specify the following
properties in their Srcfile's `Config` property:

* **GOROOT**: a directory that should be used as the GOROOT when building Go
  packages in the directory tree. If relative, it is made absolute by prefixing
  the directory containing the Srcfile.

  Setting GOROOT (to `.`) is how srclib-go builds the standard library from the
  repository `code.google.com/p/go` without having the system Go stdlib packages
  interfere with analysis.

* **GOPATH**: a colon-separated list of directories that are appended
  to the build GOPATH. If relative, the dirs are made absolute by prefixing
  the directory containing the Srcfile.

  Set GOPATH when you have vendored dependencies within your repository that you
  import using import paths relative to the vendored dir (as with godep and
  third_party.go).


## Known issues

srclib-go is alpha-quality software. It powers code analysis on
[Sourcegraph.com](https://sourcegraph.com) but has not been widely tested or
adapted for other use cases.


## Tests

TODO: needs updating.


## Contributing

Patches are welcomed via GitHub pull request! See
[CONTRIBUTING.md](./CONTRIBUTING.md) for more information.

srclib-go's type analysis is based on
[go/types](https://godoc.org/golang.org/x/tools/go/types).
