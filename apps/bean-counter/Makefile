GO ?= go
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/bean-counter

RUN_DRIVER ?= sqlite
RUN_DSN ?= file:bean-counter.db
RUN_ADDR ?= :8080
RUN_PROJECT_PREFIX ?= bean-counter
RUN_ACTOR ?= bean-counter
RUN_CORS_ORIGIN ?= http://localhost:5173

.PHONY: build vet lint fmt fmt-check test test-integration tidy-check run clean

build:
	$(GO) build ./...
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) ./cmd/bean-counter

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run ./...

fmt:
	$(GO) fmt ./...

# Driven by git ls-files rather than `find .`: a bare find descends into
# frontend/node_modules once the frontend has been installed, and hardcoding a
# directory list fails open - gofmt reports a missing directory on stderr, so
# `test -z "$$( )"` sees empty stdout and the target passes.
fmt-check:
	@files="$$(git ls-files '*.go')"; \
	  test -n "$$files" || { echo "fmt-check found no Go files; refusing to pass" >&2; exit 1; }; \
	  test -z "$$(gofmt -l $$files)" || { echo "gofmt changes needed; run make fmt" >&2; exit 1; }

test:
	$(GO) test ./...

test-integration:
	$(GO) test -tags=integration ./...

# GOWORK=off matches how the container builds this module: through the
# go.mod replace alone, with no workspace in play.
tidy-check:
	GOWORK=off $(GO) mod tidy
	git diff --exit-code go.mod go.sum

run:
	BN_DRIVER=$(RUN_DRIVER) \
	BN_DSN=$(RUN_DSN) \
	BN_ADDR=$(RUN_ADDR) \
	BN_PROJECT_PREFIX=$(RUN_PROJECT_PREFIX) \
	BN_ACTOR=$(RUN_ACTOR) \
	BN_CORS_ORIGIN=$(RUN_CORS_ORIGIN) \
	$(GO) run ./cmd/bean-counter

clean:
	rm -rf $(BIN_DIR)
