# syntax=docker/dockerfile:1
FROM golang:1.24.6-alpine3.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/whatsapp-mcp ./cmd/whatsapp-mcp

FROM alpine:3.22.1
RUN apk add --no-cache ca-certificates wget && addgroup -S app && adduser -S -G app app
COPY --from=build /out/whatsapp-mcp /usr/local/bin/whatsapp-mcp
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/whatsapp-mcp"]
