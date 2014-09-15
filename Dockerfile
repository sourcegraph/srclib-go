FROM ubuntu:14.04

RUN apt-get update -qq && echo 2014-08-12
RUN apt-get install -qq curl git mercurial bzr subversion build-essential

# Install Go

# Go 1.3:
# RUN curl -Lo /tmp/golang.tgz https://storage.googleapis.com/golang/go1.3.linux-amd64.tar.gz
# RUN tar -xzf /tmp/golang.tgz -C /usr/local

# Go 1.4:
# (use Go 1.4 to support new syntaxes (chiefly `range`))
RUN curl -Lo /tmp/golang.tgz https://go.googlecode.com/archive/e54b1af55910c77e4a215112193472f0276b3c8d.tar.gz
RUN tar -xzf /tmp/golang.tgz -C /usr/local && mv /usr/local/go-* /usr/local/go
RUN echo 'devel +e54b1a srclib' > /usr/local/go/VERSION
RUN cd /usr/local/go/src && ./make.bash

ENV GOROOT /usr/local/go
ENV GOBIN /usr/local/bin
ENV PATH /usr/local/go/bin:$PATH
ENV GOPATH /srclib

RUN go get github.com/kr/godep

# Allow determining whether we're running in Docker
ENV IN_DOCKER_CONTAINER true

# Add this toolchain
ADD . /srclib/src/sourcegraph.com/sourcegraph/srclib-go/
WORKDIR /srclib/src/sourcegraph.com/sourcegraph/srclib-go
RUN godep go install

RUN useradd -ms /bin/bash srclib
RUN mkdir /src
RUN chown -R srclib /src /srclib
USER srclib

# Now set the GOPATH for the project source code, which is mounted at /src.
ENV GOPATH /
WORKDIR /src

ENTRYPOINT ["srclib-go"]
