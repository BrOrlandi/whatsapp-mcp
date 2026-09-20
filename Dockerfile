# syntax=docker/dockerfile:1

# The build stage runs on the builder's own architecture and cross-compiles to
# the target, rather than being emulated under QEMU. Go cross-compiles a static
# binary for free, so an arm64 image builds at native speed on an amd64 runner.
FROM --platform=$BUILDPLATFORM golang:1.24.6-alpine3.22 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/whatsapp-mcp ./cmd/whatsapp-mcp

FROM alpine:3.22.1
RUN apk add --no-cache ca-certificates wget && addgroup -S app && adduser -S -G app app
COPY --from=build /out/whatsapp-mcp /usr/local/bin/whatsapp-mcp
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/whatsapp-mcp"]
