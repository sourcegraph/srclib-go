.PHONY: install

install:
	@mkdir -p .bin
	go build -o .bin/srclib-go
