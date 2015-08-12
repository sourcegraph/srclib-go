CURRENT_MAKEFILE_LIST := $(MAKEFILE_LIST)
makefileDir := $(abspath $(dir $(firstword $(CURRENT_MAKEFILE_LIST))))

.PHONY: install test gotest srctest

install:
	@mkdir -p .bin
	GOPATH="$(makefileDir)/Godeps/_workspace:$(GOPATH)" go build -o .bin/srclib-go

test: gotest srctest
	GOPATH="$(makefileDir)/Godeps/_workspace:$(GOPATH)" go test ./...
	src test -m program

gotest:
	GOPATH="$(makefileDir)/Godeps/_workspace:$(GOPATH)" go test ./...

srctest:
	git submodule update --init
	src test -m program
