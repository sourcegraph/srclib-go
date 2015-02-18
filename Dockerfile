FROM ubuntu:14.04

RUN apt-get update -qq && echo 2014-08-12
RUN apt-get install -qq curl git mercurial bzr subversion build-essential

# Install Go

# Go 1.4 devel because:
# - need to support new syntaxes (chiefly `range`)
# - the build tag file suffixes for powerpc changed to ppc64 and ppc64le
RUN curl -Lo /tmp/golang.tgz https://go.googlesource.com/go/+archive/495e02db8c6e080504f03525daffa4c8f19a7b03.tar.gz
RUN mkdir -p /usr/local/go && tar -xzf /tmp/golang.tgz -C /usr/local/go
RUN echo 'devel +495e02 srclib' > /usr/local/go/VERSION
RUN cd /usr/local/go/src && ./make.bash

# Grab Go 1.3 as well.
RUN curl -Lo /tmp/golang1.3.tgz https://storage.googleapis.com/golang/go1.3.3.linux-amd64.tar.gz
RUN mkdir -p /usr/local/go1.3 && tar -xzf /tmp/golang1.3.tgz -C /usr/local/go1.3 --strip-components=1
# HOTFIX: We need to add go1.3's GOROOT/pkg/src to the GOPATH so that
# it can be imported (because GOROOT/src/pkg was moved to GOROOT/src).
RUN mkdir -p /tmp/goroot1.3/
RUN ln -s /usr/local/go1.3/src/pkg /tmp/goroot1.3/src
RUN mv /usr/local/go1.3/bin/go /usr/local/go1.3/bin/go1.3
RUN echo '1.3.3 srclib' > /usr/local/go1.3/VERSION

ENV GOROOT /usr/local/go
ENV GOROOT13 /usr/local/go1.3
ENV GOBIN /usr/local/bin
ENV PATH /usr/local/go/bin:/usr/local/go1.3/bin:$PATH
ENV GOPATH /tmp/goroot1.3:/srclib

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
