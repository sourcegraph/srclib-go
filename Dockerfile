FROM golang:1.6-alpine

RUN apk --update add git make

ENV SRCLIBPATH $GOPATH/src
ADD . $GOPATH/src/sourcegraph.com/sourcegraph/srclib-go
RUN cd $GOPATH/src/sourcegraph.com/sourcegraph/srclib-go && make
