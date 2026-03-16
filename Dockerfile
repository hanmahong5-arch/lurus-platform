FROM oven/bun:1 AS frontend

WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ .
RUN bun run build

FROM golang:1.25-alpine AS builder

ARG GITHUB_TOKEN
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
ENV GOPRIVATE=github.com/hanmahong5-arch/*
ENV GONOSUMCHECK=github.com/hanmahong5-arch/*

RUN apk add --no-cache git && \
    if [ -n "$GITHUB_TOKEN" ]; then \
      git config --global url."https://x-access-token:${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"; \
    fi

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY --from=frontend /web/dist ./web/dist

COPY . .
RUN GOWORK=off go build -ldflags="-s -w" -trimpath -o lurus-platform-core ./cmd/core

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/lurus-platform-core /lurus-platform-core

EXPOSE 18104 18105

ENTRYPOINT ["/lurus-platform-core"]
