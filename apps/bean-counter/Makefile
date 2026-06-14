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

.PHONY: build vet lint fmt fmt-check test test-integration run clean

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

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))" || (echo "gofmt changes needed; run make fmt" >&2; exit 1)

test:
	$(GO) test ./...

test-integration:
	$(GO) test -tags=integration ./...

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
