ARG BUILDPATH='/build/'

# multi-stage build -> start w. golang:1.18-alpine for build
FROM golang:1.18-alpine as builder

ARG BUILDPATH
WORKDIR $BUILDPATH

COPY ./geocoder-svc ./

# Build the GRPC server into a binary
RUN cd ./cmd/mgmt && go build -o mgmt

# Create final image - export go binary onto regular alpine -> ~18mb
FROM alpine:latest

ARG BUILDPATH
COPY --from=builder $BUILDPATH/cmd/mgmt/ ./cmd/mgmt
