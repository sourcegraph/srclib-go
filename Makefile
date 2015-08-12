.PHONY: install test gotest srctest

install:
	GOBIN=.bin go get github.com/tools/godep
	.bin/godep go build -o .bin/srclib-go

test: gotest srctest
	.bin/godep go test ./...
	src test -m program

gotest:
	.bin/godep go test ./...

srctest:
	git submodule update --init
	src test -m program
