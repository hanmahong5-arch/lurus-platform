FROM oven/bun:1 AS frontend

WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ .
RUN bun run build

FROM golang:1.25-alpine AS builder

ARG GITHUB_TOKEN
# BUILD_SHA / BUILT_AT are injected by CI (see .github/workflows/core.yaml).
# They end up as ldflags -X overrides of internal/pkg/buildinfo's package
# vars so the running binary knows which ghcr.io/...:main-<sha7> tag it is.
# Absence is tolerated: buildinfo falls back to runtime/debug VCS info.
ARG BUILD_SHA=""
ARG BUILT_AT=""
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
RUN GOWORK=off go build \
    -ldflags="-s -w \
              -X github.com/hanmahong5-arch/lurus-platform/internal/pkg/buildinfo.sha=${BUILD_SHA} \
              -X github.com/hanmahong5-arch/lurus-platform/internal/pkg/buildinfo.built=${BUILT_AT}" \
    -trimpath -o lurus-platform-core ./cmd/core

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/lurus-platform-core /lurus-platform-core

EXPOSE 18104 18105

ENTRYPOINT ["/lurus-platform-core"]
