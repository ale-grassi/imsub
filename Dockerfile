# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    /usr/local/go/bin/go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build -trimpath -ldflags='-s -w' -o /out/imsub ./cmd/imsub

FROM alpine:3.23
RUN adduser -D -u 10001 app
USER app
WORKDIR /app
COPY --from=builder /out/imsub /app/imsub
ENV IMSUB_LISTEN_ADDR=:8080
ENV IMSUB_TWITCH_WEBHOOK_PATH=/webhooks/twitch
EXPOSE 8080
CMD ["/app/imsub"]
