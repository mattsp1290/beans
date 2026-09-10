BIN     := bin/bn
PKG     := ./cmd/bn
# Go's convention for a module in a subdirectory is subdirectory-prefixed
# tags, so this matches libs/beans/v* and ignores tags belonging to any
# application in this monorepo. --dirty now reflects the whole worktree:
# a dirty tree anywhere means this build is not reproducible from a commit.
VERSION ?= $(shell git describe --tags --match 'libs/beans/v*' --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/mattsp1290/beans/libs/beans/version.Version=$(VERSION)

.PHONY: build test vet lint tidy-check test-integration ci install clean

build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

test:
	go test ./...

# Requires Docker: the suite uses testcontainers.
test-integration:
	go test -tags=integration ./...

vet:
	go vet ./...

lint:
	golangci-lint run

# GOWORK=off is deliberate: a workspace-active tidy can resolve through a
# sibling module and write a go.sum that is incomplete for a standalone
# build, which is exactly how the container builds this module.
tidy-check:
	GOWORK=off go mod tidy
	git diff --exit-code go.mod go.sum

ci: tidy-check vet lint test build

install:
	go install -ldflags '$(LDFLAGS)' $(PKG)

# Present so the root Makefile's clean fan-out reaches every module.
clean:
	rm -rf $(dir $(BIN))
