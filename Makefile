SHELL := /bin/bash
APP := imsub
GO ?= go

.PHONY: help fmt fmt-check vet test test-integration build check ci-check deploy status logs lint cover cover-open vuln secrets-scan

help:
	@echo "Targets:"
	@echo "  make fmt      - format Go files"
	@echo "  make fmt-check - fail if Go files are not gofmt-formatted"
	@echo "  make vet      - run go vet"
	@echo "  make test     - run unit tests"
	@echo "  make test-integration - run integration-tagged tests"
	@echo "  make build    - build all packages"
	@echo "  make lint     - run golangci-lint"
	@echo "  make cover    - generate coverage.out + coverage.html"
	@echo "  make cover-open - open interactive coverage HTML view"
	@echo "  make vuln     - run govulncheck against all packages"
	@echo "  make secrets-scan - scan repository for leaked secrets (gitleaks)"
	@echo "  make check    - fmt + test + build"
	@echo "  make ci-check - run the full local equivalent of CI checks"
	@echo "  make deploy   - deploy to Fly app $(APP)"
	@echo "  make status   - show Fly app status"
	@echo "  make logs     - show recent Fly logs"

fmt:
	$(GO) fmt ./...

fmt-check:
	gofmt -l . | tee /dev/stderr | (! read)

vet:
	$(GO) vet ./...

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

ci-check: fmt-check vet build test test-integration lint vuln secrets-scan

deploy:
	flyctl deploy -a $(APP)

status:
	flyctl status -a $(APP)

logs:
	flyctl logs -a $(APP) --no-tail
