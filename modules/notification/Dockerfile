FROM golang:1.25-alpine AS builder

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags="-s -w" -trimpath -o lurus-notification ./cmd/server

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/lurus-notification /lurus-notification

EXPOSE 18900

ENTRYPOINT ["/lurus-notification"]
