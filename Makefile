.PHONY: install

install:
	mkdir .bin
	go build -o .bin/srclib-go ./cmd/src-tool-go
