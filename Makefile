# Monorepo fan-out. This file contains no build logic of its own: each module
# owns its rules, its lint policy, and its golangci-lint version.
MODULES := libs/beans apps/bean-counter

.PHONY: build test vet lint fmt-check tidy-check ci ci-integration clean beans bean-counter

build:
	@for m in $(MODULES); do $(MAKE) -C $$m build || exit 1; done

test:
	@for m in $(MODULES); do $(MAKE) -C $$m test || exit 1; done

vet:
	@for m in $(MODULES); do $(MAKE) -C $$m vet || exit 1; done

lint:
	@for m in $(MODULES); do $(MAKE) -C $$m lint || exit 1; done

# libs/beans has no fmt-check target; gofmt is enforced there by golangci-lint
# formatters. Only apps/bean-counter exposes fmt-check.
fmt-check:
	$(MAKE) -C apps/bean-counter fmt-check

tidy-check:
	@for m in $(MODULES); do $(MAKE) -C $$m tidy-check || exit 1; done

# Deliberately excludes ci-integration: both modules' integration tests use
# testcontainers and need a running Docker daemon, so the full non-integration
# gate stays runnable without one.
ci: vet lint test build tidy-check

ci-integration:
	@for m in $(MODULES); do $(MAKE) -C $$m test-integration || exit 1; done

clean:
	@for m in $(MODULES); do $(MAKE) -C $$m clean || exit 1; done

# Escape hatches: make beans TARGET=build, make bean-counter TARGET=test
beans:
	$(MAKE) -C libs/beans $(TARGET)

bean-counter:
	$(MAKE) -C apps/bean-counter $(TARGET)
