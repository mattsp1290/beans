BIN     := bin/bn
PKG     := ./cmd/bn
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/mattsp1290/beans/version.Version=$(VERSION)

.PHONY: build test vet lint tidy-check ci install

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

ci: tidy-check vet lint test build

install:
	go install -ldflags '$(LDFLAGS)' $(PKG)
