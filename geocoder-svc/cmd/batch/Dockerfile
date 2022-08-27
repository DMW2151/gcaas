ARG BUILDPATH='/build/'

# multi-stage build -> start w. golang:1.18-alpine for build
FROM golang:1.18-alpine as builder

ARG BUILDPATH
WORKDIR $BUILDPATH

COPY ./geocoder-svc ./

# Build the GRPC server into a binary
RUN cd ./cmd/batch && go build -o batch

# Create final image - export go binary onto regular alpine -> ~18mb
FROM alpine:latest

ARG BUILDPATH
COPY --from=builder $BUILDPATH/cmd/batch/ ./cmd/batch
