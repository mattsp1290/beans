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

# Transitional immutable Go oracle. Candidate binaries must use the recorded
# VERSION=migration-oracle and placeholder UI until asset contracts are added.
REFERENCE_BINARY ?= .compat/reference/bn-go
CANDIDATE_BINARY ?= $(REFERENCE_BINARY)
.PHONY: compat-reference compat-assets-reference compat-assets compat-cli compat-http compat-journey compat-signals compat-io compat-test compat-frontmatter compat-domain-read
compat-reference:
	python3 -S tools/compat/build_reference.py

compat-assets-reference:
	python3 -S tools/compat/build_reference.py --ui-build --output .compat/reference-assets

ASSETS_BINARY ?= .compat/reference-assets/bn-go
compat-assets:
	python3 -S tools/compat/assets.py check --binary '$(ASSETS_BINARY)'

compat-cli:
	python3 -S tools/compat/runner.py check --binary '$(CANDIDATE_BINARY)'

compat-http:
	python3 -S tools/compat/http_runner.py check --binary '$(CANDIDATE_BINARY)'
	python3 -S tools/compat/http_runner.py check --binary '$(CANDIDATE_BINARY)' --corpus tests/contract/http-broken.json
	python3 -S tools/compat/http_journey.py check --binary '$(CANDIDATE_BINARY)'
	python3 -S tools/compat/http_journey.py check --binary '$(CANDIDATE_BINARY)' --corpus tests/contract/http-journey-pushed.json

compat-journey:
	python3 -S tools/compat/journey.py check --binary '$(CANDIDATE_BINARY)'
	python3 -S tools/compat/journey.py check --binary '$(CANDIDATE_BINARY)' --corpus tests/contract/journey-pushed.json
	python3 -S tools/compat/journey.py check --binary '$(CANDIDATE_BINARY)' --corpus tests/contract/journey-text.json
	python3 -S tools/compat/journey.py check --binary '$(CANDIDATE_BINARY)' --corpus tests/contract/journey-text-pushed.json
	python3 -S tools/compat/journey.py check --binary '$(CANDIDATE_BINARY)' --corpus tests/contract/journey-files.json
	python3 -S tools/compat/journey.py check --binary '$(CANDIDATE_BINARY)' --corpus tests/contract/journey-files-text.json

compat-signals:
	python3 -S tools/compat/signals.py check --binary '$(CANDIDATE_BINARY)'

compat-io:
	python3 -S tools/compat/io_failures.py check --binary '$(CANDIDATE_BINARY)'

compat-test:
	BN_REFERENCE_BINARY='$(REFERENCE_BINARY)' python3 -S -m unittest discover -s tools/compat -p 'test_*.py' -v

compat-domain-read:
	python3 -S tools/compat/cross_read.py

compat-frontmatter:
	python3 -S tools/compat/frontmatter_reference.py --check tests/contract/frontmatter-primitives.json

.PHONY: compat-templates
compat-templates:
	python3 -S tools/compat/template_reference.py --check tests/contract/templates.json

.PHONY: compat-ids
compat-ids:
	python3 -S tools/compat/id_reference.py --check tests/contract/ids.json

.PHONY: compat-request-lifecycle
compat-request-lifecycle:
	python3 -S tools/compat/request_lifecycle_reference.py --check tests/contract/request-lifecycle.json

.PHONY: compat-requests compat-request-read
compat-requests:
	python3 -S tools/compat/request_reference.py --check tests/contract/requests.json

compat-request-read:
	python3 -S tools/compat/request_cross_read.py

.PHONY: compat-memories compat-memory-read
compat-memories:
	python3 -S tools/compat/memory_reference.py --check tests/contract/memories.json

compat-memory-read:
	python3 -S tools/compat/memory_cross_read.py

.PHONY: compat-handoffs compat-handoff-read
compat-handoffs:
	python3 -S tools/compat/handoff_reference.py --check tests/contract/handoffs.json

compat-handoff-read:
	python3 -S tools/compat/handoff_cross_read.py

.PHONY: compat-plan-foundation
compat-plan-foundation:
	python3 -S tools/compat/plan_foundation_reference.py --check tests/contract/plan-foundation.json

.PHONY: compat-plan-graph
compat-plan-graph:
	python3 -S tools/compat/plan_graph_reference.py --check tests/contract/plan-graph.json

.PHONY: verify-toolchain verify-kernel
verify-toolchain:
	python3 -S tools/verification/qualify.py

verify-kernel:
	python3 -S tools/verification/kernel.py

.PHONY: rust-build rust-test rust-check
rust-build:
	cargo build --workspace --locked

rust-test:
	cargo test --workspace --locked

rust-check:
	cargo fmt --all --check
	cargo clippy --workspace --all-targets --locked -- -D warnings

.PHONY: compat-plan-encode compat-plan-encode-read
compat-plan-encode:
	python3 -S tools/compat/plan_encode_reference.py --check tests/contract/plan-encode.json

compat-plan-encode-read:
	python3 -S tools/compat/plan_encode_read.py

.PHONY: compat-plan-parse
compat-plan-parse:
	python3 -S tools/compat/plan_parse_reference.py --check tests/contract/plan-parse.json

.PHONY: compat-plan-ref
compat-plan-ref:
	python3 -S tools/compat/plan_ref_reference.py --check tests/contract/plan-ref.json

.PHONY: compat-plan-ref-read
compat-plan-ref-read:
	python3 -S tools/compat/plan_ref_read.py

.PHONY: compat-plan-bundle compat-plan-bundle-read
compat-plan-bundle:
	python3 -S tools/compat/plan_bundle_reference.py --check tests/contract/plan-bundle.json

compat-plan-bundle-read:
	python3 -S tools/compat/plan_bundle_read.py

.PHONY: compat-config-foundation
compat-config-foundation:
	python3 -S tools/compat/config_foundation_reference.py --check tests/contract/config-foundation.json

.PHONY: compat-config-codec compat-config-codec-read
compat-config-codec:
	python3 -S tools/compat/config_codec_reference.py --check tests/contract/config-codec.json
compat-config-codec-read:
	python3 -S tools/compat/config_codec_read.py

.PHONY: compat-workflow-file compat-workflow-file-read
compat-workflow-file:
	python3 -S tools/compat/workflow_file_reference.py --check tests/contract/workflow-file.json
compat-workflow-file-read:
	python3 -S tools/compat/workflow_file_read.py

.PHONY: compat-paths
compat-paths:
	python3 -S tools/compat/paths_reference.py --check tests/contract/paths.json

.PHONY: compat-remote
compat-remote:
	python3 -S tools/compat/remote_reference.py --check tests/contract/remote.json

.PHONY: compat-resolve
compat-resolve:
	python3 -S tools/compat/resolve_reference.py --check tests/contract/resolve.json

.PHONY: compat-context
compat-context:
	python3 -S tools/compat/context_reference.py --check tests/contract/context.json

.PHONY: compat-index-graph
compat-index-graph:
	python3 -S tools/compat/graph_reference.py --check tests/contract/graph.json
