ifeq ($(OS),Windows_NT)
	SRCLIB_GO_EXE := .bin/srclib-go.exe
else
	SRCLIB_GO_EXE := .bin/srclib-go
endif

.PHONY: install test gotest srctest release

install: ${SRCLIB_GO_EXE}

${SRCLIB_GO_EXE}: $(shell /usr/bin/find . -type f -and -name '*.go' -not -path './Godeps/*')
	GOBIN=$(CURDIR)/.bin go get github.com/tools/godep
	.bin/godep go build -o ${SRCLIB_GO_EXE}

test: gotest srctest
	.bin/godep go test ./...
	src test -m program

gotest:
	.bin/godep go test ./...

srctest:
# go1.5 excludes repos whose ImportPath would include testdata. Since all the
# test repos are under testdata dir, we change the GOPATH to not root the
# testdata dir
	git submodule update --init
	GOPATH=${PWD}/.test go get -d golang.org/x/net/ipv6 golang.org/x/tools/go/types
	GOPATH=${PWD}/.test src test -m program

release:
	docker build -t srclib/srclib-go .
