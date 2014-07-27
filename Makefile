CURRENT_MAKEFILE_LIST := $(MAKEFILE_LIST)
makefileDir := $(dir $(firstword $(CURRENT_MAKEFILE_LIST)))

.PHONY: install

install:
	@mkdir -p .bin
# TODO(sqs): need to set GOPATH and GOBIN for this (can use makefileDir to get
# the dir containing this Makefile)
	go build -o .bin/srclib-go
