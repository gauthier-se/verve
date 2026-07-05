# Verve — developer task runner.
#
# `make` with no target prints this help. The `ci` target mirrors
# .github/workflows/ci.yml exactly, so a green `make ci` means a green CI.

BINARY      := verve
BIN_DIR     := bin
CMD         := ./cmd/verve
DATA_DIR    ?= ./data
# Args forwarded to `make run`, e.g. `make run ARGS="account create --email=me@x"`.
ARGS        ?=

GO          := go

.DEFAULT_GOAL := help

## help: list the available targets
.PHONY: help
help:
	@echo "Verve — make targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | awk -F': ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

## build: compile the binary into bin/
.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(BINARY) $(CMD)

## run: build and run the binary (pass flags via ARGS="...")
.PHONY: run
run: build
	./$(BIN_DIR)/$(BINARY) -data-dir=$(DATA_DIR) $(ARGS)

## test: run the full test suite with the race detector
.PHONY: test
test:
	$(GO) test -race ./...

## test-short: run tests without the race detector (faster)
.PHONY: test-short
test-short:
	$(GO) test ./...

## cover: run tests and open the HTML coverage report
.PHONY: cover
cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out

## fmt: format all Go files in place
.PHONY: fmt
fmt:
	gofmt -w .

## fmt-check: fail if any Go file is not gofmt-formatted (as CI does)
.PHONY: fmt-check
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

## vet: run go vet
.PHONY: vet
vet:
	$(GO) vet ./...

## tidy: sync go.mod / go.sum with the source
.PHONY: tidy
tidy:
	$(GO) mod tidy

## ci: run the same checks as CI (fmt-check, vet, build, test -race)
.PHONY: ci
ci: fmt-check vet
	$(GO) build ./...
	$(GO) test -race ./...

## clean: remove build and coverage artifacts
.PHONY: clean
clean:
	rm -rf $(BIN_DIR) coverage.out
