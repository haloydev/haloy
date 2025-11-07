.PHONY: test lint fmt check-fmt ci

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
