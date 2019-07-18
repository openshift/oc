FROM golang:1.11-alpine

RUN apk update && \
    apk add make git protobuf

ENV MODULE github.com/golang/protobuf
ENV SRC ${GOPATH}/src/${MODULE}
COPY vendor/${MODULE} ${SRC}
RUN echo $(ls ${SRC})  
RUN go install ${SRC}/proto ${SRC}/ptypes ${SRC}/protoc-gen-go


WORKDIR /codegen

COPY pkg pkg
COPY Makefile Makefile
RUN make codegen

LABEL maintainer="Odin Team <aos-odin@redhat.com>"