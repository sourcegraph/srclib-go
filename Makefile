ifeq ($(OS),Windows_NT)
	SRCLIB_GO_EXE := .bin/srclib-go.exe
else
	SRCLIB_GO_EXE := .bin/srclib-go
endif

.PHONY: install test gotest srctest

install: ${SRCLIB_GO_EXE}

${SRCLIB_GO_EXE}: $(shell /usr/bin/find . -type f -and -name '*.go' -not -path './Godeps/*')
	GOBIN=.bin go get github.com/tools/godep
	.bin/godep go build -o ${SRCLIB_GO_EXE}

test: gotest srctest
	.bin/godep go test ./...
	src test -m program

gotest:
	.bin/godep go test ./...

srctest:
	git submodule update --init
	src test -m program
