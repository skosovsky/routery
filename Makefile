GO      := go
MODULES := $(shell find . -type d \( -name ".*" -not -name "." -o -name "vendor" \) -prune -o -type f -name "go.mod" -exec dirname {} \;)

.PHONY: lint fix test bench bench-hotpath fuzz cover release-patch release-break

lint:
	@for dir in $(MODULES); do \
		echo "golangci-lint - $$dir"; \
		(cd "$$dir" && golangci-lint run ./...) || exit 1; \
	done

fix:
	@if [ -f "go.work" ]; then $(GO) work sync; fi
	@for dir in $(MODULES); do \
		echo "fix & tidy - $$dir"; \
		(cd "$$dir" && $(GO) fix ./... && $(GO) mod tidy) || exit 1; \
		(cd "$$dir" && golangci-lint run --fix ./...) || exit 1; \
	done

test:
	@for dir in $(MODULES); do \
		echo "test - $$dir"; \
		(cd "$$dir" && $(GO) test -v -race ./...) || exit 1; \
	done

bench:
	@for dir in $(MODULES); do \
		echo "bench - $$dir"; \
		(cd "$$dir" && $(GO) test -bench=. -run=^$$ ./...) || exit 1; \
	done

FUZZTIME ?= 30s
FUZZ_MODULES ?= $(MODULES)

fuzz:
	@for dir in $(FUZZ_MODULES); do \
		echo "fuzz - $$dir"; \
		(cd "$$dir" && GO="$(GO)" FUZZTIME="$(FUZZTIME)" sh "$(CURDIR)/scripts/fuzz.sh") || exit 1; \
	done

cover:
	@for dir in $(MODULES); do \
		echo "cover - $$dir"; \
		(cd "$$dir" && $(GO) test -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out) || exit 1; \
	done

release-patch: lint test ## v0.5.0 -> v0.5.1
	@chmod +x ./scripts/release.sh
	@./scripts/release.sh patch "$(MODULES)"

release-break: lint test ## v0.5.1 -> v0.6.0
	@chmod +x ./scripts/release.sh
	@./scripts/release.sh break "$(MODULES)"

.PHONY: stream-conformance
stream-conformance:
	@STREAM_SOURCE_DIR="$(STREAM_SOURCE_DIR)" STREAM_SOURCE_REF="$(STREAM_SOURCE_REF)" python3 scripts/stream-conformance.py
