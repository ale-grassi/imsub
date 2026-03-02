SHELL := /bin/bash
APP := imsub
GO ?= go

.PHONY: help fmt test test-integration build check deploy status logs lint cover cover-open vuln secrets-scan

help:
	@echo "Targets:"
	@echo "  make fmt      - format Go files"
	@echo "  make test     - run unit tests"
	@echo "  make test-integration - run integration-tagged tests"
	@echo "  make build    - build all packages"
	@echo "  make lint     - run golangci-lint"
	@echo "  make cover    - generate coverage.out + coverage.html"
	@echo "  make cover-open - open interactive coverage HTML view"
	@echo "  make vuln     - run govulncheck against all packages"
	@echo "  make secrets-scan - scan repository for leaked secrets (gitleaks)"
	@echo "  make check    - fmt + test + build"
	@echo "  make deploy   - deploy to Fly app $(APP)"
	@echo "  make status   - show Fly app status"
	@echo "  make logs     - show recent Fly logs"

fmt:
	$(GO) fmt ./...

test:
	$(GO) test -race -count=1 ./...

test-integration:
	$(GO) test -race -count=1 -tags=integration ./tests/integration/...

build:
	$(GO) build ./...

lint:
	golangci-lint run ./...

cover:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

cover-open:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

secrets-scan:
	gitleaks detect --no-banner --redact --source=.

check: fmt test build

deploy:
	flyctl deploy -a $(APP)

status:
	flyctl status -a $(APP)

logs:
	flyctl logs -a $(APP) --no-tail
