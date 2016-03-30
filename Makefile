SGX_OS_NAME := $(shell uname -o 2>/dev/null || uname -s)

ifeq "$(SGX_OS_NAME)" "Cygwin"
	CMD := cmd /C
else
	ifeq "$(SGX_OS_NAME)" "Msys"
		CMD := cmd //C
	else
	ifneq (,$(findstring MINGW, $(SGX_OS_NAME)))
		CMD := cmd //C
	endif
	endif
endif

ifeq ($(OS),Windows_NT)
	SRCLIB_GO_EXE := .bin/srclib-go.exe
	CURDIR := $(shell $(CMD) "echo %cd%")
	CURDIR := $(subst \,/,$(CURDIR))
	PWD := $(CURDIR)
else
	SRCLIB_GO_EXE := .bin/srclib-go
endif

.PHONY: install test gotest srctest

default: govendor install

install: ${SRCLIB_GO_EXE}

govendor:
	go get github.com/kardianos/govendor
	govendor sync

${SRCLIB_GO_EXE}: $(shell /usr/bin/find . -type f -and -name '*.go' -not -path './vendor/*')
	go build -o ${SRCLIB_GO_EXE}

test: gotest srctest

gotest:
	go test $(shell go list ./... | grep -v /vendor/)

srctest:
# go1.5 excludes repos whose ImportPath would include testdata. Since all the
# test repos are under testdata dir, we change the GOPATH to not root the
# testdata dir
	git submodule update --init
	GOPATH=${PWD}/.test go get -d golang.org/x/net/ipv6 golang.org/x/tools/go/types
	GOPATH=${PWD}/testdata/case:${PWD}/.test srclib test

