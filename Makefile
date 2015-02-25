CURRENT_MAKEFILE_LIST := $(MAKEFILE_LIST)
makefileDir := $(dir $(firstword $(CURRENT_MAKEFILE_LIST)))

.PHONY: install test gotest srctest

install:
	@mkdir -p .bin
	go get github.com/tools/godep
	godep go build -o .bin/srclib-go

test: gotest srctest
	godep go test ./...
	src test -m program

gotest:
	godep go test ./...

srctest:
	git submodule update --init
	src test -m program
