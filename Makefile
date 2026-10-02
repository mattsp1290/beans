.NOTPARALLEL:
BIN_DIR := bin
BIN := $(BIN_DIR)/bn
CARGO_TARGET_DIR ?= target
VERSION ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
export CARGO_TARGET_DIR
export BN_VERSION = $(VERSION)

.PHONY: build test vet lint tidy-check ci release-build install clean ui-install ui-test ui-check ui-build verify-native verify-toolchain verify-kernel verify-codec-coupling verify-retry-coupling verify skill-test

# UI assets are embedded at compile time. Plain build uses the checked-in
# placeholder; release-build and ci build the complete app first.
build:
	cargo build --workspace --release --locked
	mkdir -p $(BIN_DIR)
	cp -f $(CARGO_TARGET_DIR)/release/bn $(BIN)

test:
	cargo test --workspace --locked

vet:
	cargo check --workspace --all-targets --locked

lint:
	cargo fmt --all --check
	cargo clippy --workspace --all-targets --locked -- -D warnings

tidy-check:
	cargo metadata --locked --format-version 1 > /dev/null
	git diff --exit-code Cargo.toml Cargo.lock crates/beans-kernel/Cargo.toml

ui-install:
	cd ui && npm ci
ui-test:
	cd ui && npm run test
ui-check:
	cd ui && npm run check
ui-build:
	cd ui && npm run build

# Recursive steps also preserve asset/build ordering under make -j.
ci:
	$(MAKE) ui-install ui-test ui-check ui-build
	$(MAKE) vet lint test build tidy-check skill-test

release-build:
	$(MAKE) ui-install ui-build
	$(MAKE) build

# Build the complete UI before installing to Cargo's configured bin directory.
install:
	$(MAKE) ui-install ui-build
	cargo install --locked --path . --force

clean:
	cargo clean
	rm -rf $(BIN_DIR) ui/node_modules
	find ui/dist -mindepth 1 ! -name index.html -delete
	git restore -- ui/dist/index.html

skill-test:
	python3 -S .agents/skills/bn-plan-loop/evals/run_tests.py
	python3 -S .agents/skills/bn-build/evals/run_tests.py

verify-native:
	python3 -S tools/verification/native.py
verify-toolchain:
	python3 -S tools/verification/qualify.py
verify-kernel:
	python3 -S tools/verification/kernel.py
verify-codec-coupling:
	python3 -S tools/verification/codec_coupling.py
verify-retry-coupling:
	python3 -S tools/verification/retry_coupling.py
verify:
	$(MAKE) verify-native
	$(MAKE) verify-toolchain
	$(MAKE) verify-kernel
	$(MAKE) verify-codec-coupling
	$(MAKE) verify-retry-coupling
