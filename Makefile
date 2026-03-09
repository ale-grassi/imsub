SHELL := /bin/bash
APP := imsub
GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck
GITLEAKS ?= gitleaks
export GOCACHE ?= /tmp/gocache
export GOLANGCI_LINT_CACHE ?= /tmp/golangci-lint

.PHONY: help fmt fmt-check vet test test-integration build check ci-check deploy status logs lint style-check cover cover-open vuln secrets-scan redis-proxy

help:
	@echo "Targets:"
	@echo "  make fmt      - format Go files with gofmt and golangci-lint fmt (goimports)"
	@echo "  make fmt-check - fail if Go files need gofmt or golangci-lint fmt (goimports)"
	@echo "  make vet      - run go vet"
	@echo "  make test     - run unit tests"
	@echo "  make test-integration - run integration-tagged tests"
	@echo "  make build    - build all packages"
	@echo "  make lint     - run golangci-lint"
	@echo "  make style-check - run local style checks aligned with google-go-styleguide.md"
	@echo "  make cover    - generate coverage.out + coverage.html"
	@echo "  make cover-open - open interactive coverage HTML view"
	@echo "  make vuln     - run govulncheck against all packages"
	@echo "  make secrets-scan - scan repository for leaked secrets (gitleaks)"
	@echo "  make redis-proxy - open an interactive Fly Redis proxy"
	@echo "  make check    - fmt + test + build"
	@echo "  make ci-check - run the full local equivalent of CI checks"
	@echo "  make deploy   - deploy to Fly app $(APP)"
	@echo "  make status   - show Fly app status"
	@echo "  make logs     - show recent Fly logs"

fmt:
	find . -type f -name '*.go' -not -path './vendor/*' -print0 | xargs -0 gofmt -w
	$(GOLANGCI_LINT) fmt

fmt-check:
	@out="$$(find . -type f -name '*.go' -not -path './vendor/*' -exec gofmt -l {} +)"; \
	if [ -n "$$out" ]; then \
		echo "$$out"; \
		exit 1; \
	fi
	$(GOLANGCI_LINT) fmt --diff

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -count=1 ./...

test-integration:
	$(GO) test -race -count=1 -tags=integration ./tests/integration/...
	
build:
	$(GO) build ./...

lint:
	$(GOLANGCI_LINT) run

style-check: fmt-check lint

cover:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

cover-open:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out

vuln:
	@if command -v $(GOVULNCHECK) >/dev/null 2>&1; then \
		$(GOVULNCHECK) ./...; \
	else \
		$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...; \
	fi

secrets-scan:
	$(GITLEAKS) detect --no-banner --redact --source=.

redis-proxy:
	flyctl redis proxy

check: fmt test build

ci-check: fmt-check vet build test test-integration lint vuln secrets-scan

deploy:
	flyctl deploy -a $(APP)

status:
	flyctl status -a $(APP)

logs:
	flyctl logs -a $(APP) --no-tail
