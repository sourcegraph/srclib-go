CURRENT_MAKEFILE_LIST := $(MAKEFILE_LIST)
makefileDir := $(dir $(firstword $(CURRENT_MAKEFILE_LIST)))

.PHONY: install

install:
	@mkdir -p .bin
	go get -d ./...
	go build -o .bin/srclib-go
