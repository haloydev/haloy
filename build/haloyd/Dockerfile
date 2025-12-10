FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

# Build arguments for cross-compilation
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

# Install build dependencies
RUN apk add --no-cache \
    git \
    gcc \
    musl-dev \
    ca-certificates

WORKDIR /app

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build with optimizations and version info
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X 'github.com/haloydev/haloy/internal/constants.Version=${VERSION}'" \
    -a \
    -o /haloyd ./cmd/haloyd

# Docker CLI download stage - runs on build platform, downloads for target
FROM --platform=$BUILDPLATFORM alpine:latest AS docker-cli

ARG TARGETARCH

# Map TARGETARCH to Docker's naming convention and download
RUN apk add --no-cache curl && \
    DOCKER_VERSION="27.3.1" && \
    case "${TARGETARCH}" in \
        amd64) DOCKER_ARCH="x86_64" ;; \
        arm64) DOCKER_ARCH="aarch64" ;; \
        *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    curl -fsSL "https://download.docker.com/linux/static/stable/${DOCKER_ARCH}/docker-${DOCKER_VERSION}.tgz" -o /tmp/docker.tgz && \
    tar -xzf /tmp/docker.tgz -C /tmp && \
    mv /tmp/docker/docker /docker

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -s /bin/sh appuser

# Copy Docker CLI (static binary)
COPY --from=docker-cli /docker /usr/local/bin/docker

# Copy the binary
COPY --from=builder /haloyd /usr/local/bin/haloyd

# Change to non-root user
USER appuser

ENTRYPOINT ["/usr/local/bin/haloyd"]
