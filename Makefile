.DEFAULT_GOAL := help

BIN      := jz
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X github.com/nao1215/jsonize/internal/buildinfo.Version=$(VERSION)
GOLANGCI := v2.13.2
PKGS     := ./...

.PHONY: build
build: ## Build the jz binary into ./dist
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BIN) ./cmd/jz

.PHONY: install
install: ## Install jz into $(go env GOPATH)/bin
	CGO_ENABLED=0 go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/jz

.PHONY: clean
clean: ## Remove build and test artifacts
	rm -rf dist cover.out cover.html bench/new.txt

.PHONY: fmt
fmt: ## Format Go code with gofmt
	gofmt -s -w .

.PHONY: vet
vet: ## Run go vet
	go vet $(PKGS)

.PHONY: lint
lint: ## Run golangci-lint for linux, darwin and windows
	@for os in linux darwin windows; do echo "== GOOS=$$os"; GOOS=$$os golangci-lint run ./... || exit 1; done

.PHONY: test
test: ## Run unit and golden tests with coverage
	go test -cover -coverpkg=$(PKGS) -coverprofile=cover.out $(PKGS)

.PHONY: test-race
test-race: ## Run unit tests with the race detector
	go test -race -timeout 10m $(PKGS)

.PHONY: coverage
coverage: test ## Produce cover.html from the coverage profile
	go tool cover -html=cover.out -o cover.html
	go tool cover -func=cover.out | tail -1

.PHONY: registry-test
registry-test: ## Validate parser definitions and run their golden tests (DIR=path for a local registry)
	go test ./registry $(if $(DIR),-registry-dir $(DIR),) -count=1

.PHONY: registry-update-golden
registry-update-golden: ## Rewrite the expected JSON of every fixture, then review `git diff`
	go test ./registry -update $(if $(DIR),-registry-dir $(DIR),) -count=1
	@echo "golden files rewritten; review them with: git diff"

.PHONY: fuzz
fuzz: ## Run every fuzz target briefly (FUZZTIME=10s)
	@for t in $$(grep -rhoE 'func (Fuzz[A-Za-z0-9_]+)' --include='*_test.go' . | awk '{print $$2}' | sort -u); do \
		pkg=./$$(grep -rlE "func $$t\(" --include='*_test.go' . | head -1 | xargs dirname); \
		echo "== $$t ($$pkg)"; go test -run '^$$' -fuzz "^$$t$$" -fuzztime $${FUZZTIME:-10s} $$pkg || exit 1; \
	done

.PHONY: bench
bench: ## Run benchmarks (COUNT=6) and store the result in bench/new.txt
	go test -run '^$$' -bench . -benchmem -count $${COUNT:-6} ./internal/... | tee bench/new.txt

.PHONY: bench-compare
bench-compare: ## Compare bench/new.txt against bench/baseline.txt with benchstat
	go run golang.org/x/perf/cmd/benchstat@latest bench/baseline.txt bench/new.txt

.PHONY: e2e
e2e: build ## Run the atago end-to-end suite (requires atago on PATH)
	bash ./scripts/run_e2e.sh

.PHONY: website
website: ## Build the documentation site into website/public
	cd website && hugo --minify

.PHONY: website-serve
website-serve: ## Serve the documentation site locally
	cd website && hugo server --buildDrafts

.PHONY: demo
demo: build ## Re-record demo/jsonize.gif with vhs
	vhs demo/jsonize.tape

.PHONY: check
check: fmt vet lint test test-race ## Run everything CI runs locally except E2E

.PHONY: tools
tools: ## Install developer tools (golangci-lint, atago, goreleaser)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI)
	go install github.com/nao1215/atago@latest
	go install github.com/goreleaser/goreleaser/v2@latest

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
