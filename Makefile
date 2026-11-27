# chezmoi-dots: The Developer Companion Plugin for Chezmoi
GOLANGCI := go tool -modfile=golangci-lint.mod golangci-lint
LINT_GOOS ?= linux darwin windows

include help-tests.mk

.PHONY: help fmt lint vuln test test-action check check-fast build install readme-images

help: ## List targets
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "%-18s %s\n", $$1, $$2}'

fmt: ## Format, tidy and apply modernizers
	go mod tidy
	go fix ./...
	$(GOLANGCI) fmt

lint: ## Tidy check, then format check and lint per target OS, and shellcheck of the action scripts
	go mod tidy -diff
	@bin=$$(go tool -n -modfile=golangci-lint.mod golangci-lint) && \
	for os in $(LINT_GOOS); do echo "golangci-lint run (GOOS=$$os)"; GOOS=$$os $$bin run || exit 1; done
	shellcheck .github/actions/*/*.sh

vuln: ## Reachable known vulnerabilities (needs the network)
	go tool govulncheck ./...

test: ## Unit tests with the race detector, in random order
	go test -race -shuffle=on -coverprofile=coverage.out ./...

test-action: ## Hermetic tests of the pkg-bump action script
	bash .github/actions/pkg-bump/pkg-bump_test.sh

check: lint test test-action ## lint + test + test-action

check-fast: ## Tests without the race detector
	go test -shuffle=on ./...

build: ## Static binary into bin/
	CGO_ENABLED=0 go build -trimpath -o bin/chezmoi-dots ./cmd/chezmoi-dots

install: build ## Put the binary on PATH (~/.local/bin)
	install -d $(HOME)/.local/bin
	install bin/chezmoi-dots $(HOME)/.local/bin/chezmoi-dots

readme-images: ## Frame raw screenshots with macOS canvas styling for README
	@if [ -f scripts/readme-images.mjs ]; then node scripts/readme-images.mjs; fi
