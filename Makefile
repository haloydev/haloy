.PHONY: tools test lint fmt check-fmt ci-test build build-haloy build-haloyd

VERSION ?= $(shell git describe --tags --dirty --always --match 'v*' 2>/dev/null || echo dev)
GO_LDFLAGS := -s -w -X github.com/haloydev/haloy/internal/constants.Version=$(VERSION)

# Install required tools
tools:
	go install mvdan.cc/gofumpt@latest

# Run tests
test:
	go test -v ./...

# Run linting
lint:
	go vet ./...

# Format code
fmt:
	gofumpt -w .

# Check if code is formatted (same as CI)
check-fmt:
	@if [ "$$(gofumpt -l .)" != "" ]; then \
		echo "The following files are not properly formatted:"; \
		gofumpt -l .; \
		echo "Run 'make fmt' to fix formatting issues"; \
		exit 1; \
	fi

# Run all CI checks locally
ci-test: test lint check-fmt
	@echo "All checks passed! ✅"

build: build-haloy build-haloyd

build-haloy:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o bin/haloy ./cmd/haloy

build-haloyd:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o bin/haloyd ./cmd/haloyd
