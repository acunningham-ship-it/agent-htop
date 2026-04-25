.PHONY: build test clean help version

VERSION := v0.3.0
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.GitSHA=$(GIT_SHA)"

help:
	@echo "agent-htop Makefile targets:"
	@echo "  make build      - Build the binary"
	@echo "  make test       - Run tests"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make version    - Show version info"

version:
	@echo "agent-htop $(VERSION) (git: $(GIT_SHA))"

build:
	@echo "Building agent-htop $(VERSION) (git: $(GIT_SHA))..."
	go build $(LDFLAGS) -o agent-htop ./cmd/agent-htop

test:
	go test -v ./...

clean:
	rm -f agent-htop
	go clean
