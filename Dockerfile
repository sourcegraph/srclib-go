FROM ubuntu:14.04

RUN apt-get update -qq
RUN apt-get install -qq curl git mercurial

# Install Go
RUN curl -Lo /tmp/golang.tgz https://storage.googleapis.com/golang/go1.3.linux-amd64.tar.gz
RUN tar -xzf /tmp/golang.tgz -C /usr/local
ENV GOROOT /usr/local/go
ENV GOBIN /usr/local/bin
ENV PATH /usr/local/go/bin:$PATH
ENV GOPATH /srclib

# TMP: this is slow, so pre-fetch it
RUN go get github.com/golang/gddo/gosrc github.com/sourcegraph/go-vcsurl code.google.com/p/go.tools/go/loader

# TMP: use the ext-toolchains branch of srclib
RUN mkdir -p /srclib/src/github.com/sourcegraph
RUN cd /srclib/src/github.com/sourcegraph && git clone https://github.com/sourcegraph/srclib.git
RUN cd /srclib/src/github.com/sourcegraph/srclib && git fetch origin ext-toolchains && git checkout ext-toolchains && git checkout b96226 && git branch -D master && git checkout -b master --track origin/ext-toolchains

# Add this toolchain
ADD . /srclib/src/github.com/sourcegraph/srclib-go/
WORKDIR /srclib/src/github.com/sourcegraph/srclib-go
RUN go get -v -d ./cmd/src-tool-go
RUN go install ./cmd/src-tool-go

WORKDIR /src
ENTRYPOINT ["src-tool-go"]
