# Multi-stage Dockerfile for building producer and sorter binaries

# ---------- Build Stage ----------
FROM golang:1.21-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git bash build-base

# Cache deps (download module graph)
COPY go.mod ./
COPY go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source
COPY . .

# Ensure go.sum is up-to-date inside the builder (fixes missing sums during build)
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod tidy

# Build binaries with optimizations
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/producer ./cmd/producer && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/sorter ./cmd/sorter

# ---------- Final Stage ----------
FROM alpine:latest AS final
WORKDIR /app
RUN apk add --no-cache ca-certificates bash coreutils

COPY --from=builder /out/producer /app/producer
COPY --from=builder /out/sorter /app/sorter
COPY scripts/ /app/scripts/

# Ensure scripts are executable
RUN chmod +x /app/scripts/*.sh || true

ENV PATH="/app:/app/scripts:${PATH}"
CMD ["/bin/bash"]


