BIN_DIR := bin
BIN     := $(BIN_DIR)/bn
PKG     := ./cmd/bn
VERSION ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/mattsp1290/beans/version.Version=$(VERSION)

.PHONY: build test vet lint tidy-check ui-install ui-test ui-check ui-build ci release-build install clean

# build embeds whatever ui/dist/ holds and never needs Node; run ui-build
# first (or `make ci` / `make release-build`) for a binary with the real UI.
build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

tidy-check:
	go mod tidy
	git diff --exit-code go.mod go.sum

ui-install:
	cd ui && npm ci

ui-test:
	cd ui && npm run test

ui-check:
	cd ui && npm run check

ui-build:
	cd ui && npm run build

# Order matters: the UI is built before `build` so the binary embeds the UI
# produced in the same run.
ci: ui-install ui-test ui-check ui-build vet lint test build tidy-check

release-build: ui-install ui-build build

install:
	go install -ldflags '$(LDFLAGS)' $(PKG)

clean:
	rm -rf $(BIN_DIR)
	cd ui && rm -rf node_modules
	find ui/dist -mindepth 1 ! -name index.html -delete
