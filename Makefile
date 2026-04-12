SHELL := /bin/bash
APP := imsub
GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck
GITLEAKS ?= gitleaks
export GOPATH := /tmp/gopath
export GOMODCACHE := /tmp/gomodcache
export GOCACHE := /tmp/gocache
export GOLANGCI_LINT_CACHE := /tmp/golangci-lint
TUNNEL_URL_FILE := tmp/ngrok-url
TUNNEL_PID_FILE := tmp/ngrok.pid
TUNNEL_LOG_FILE := tmp/ngrok.log
NGROK_API_URL := http://127.0.0.1:4040/api/tunnels

.PHONY: help run run-tunnel tunnel attach-tunnel stop-tunnel seed fmt fmt-check vet test test-integration build check ci-check lint style-check cover cover-open vuln fly-config-validate secrets-scan msg-gallery msg-gallery-md msg-gallery-tg prod-logs prod-redis-proxy

help:
	@echo "Targets:"
	@echo "  make run      - run ImSub locally inside the devcontainer using .env.dev"
	@echo "  make run-tunnel [PUBLIC_BASE_URL=https://...] - run ImSub in webhook mode using .env.dev and an existing or auto-started ngrok tunnel"
	@echo "  make tunnel   - start an ngrok tunnel for http://localhost:8080 and cache the URL in tmp/ngrok-url"
	@echo "  make attach-tunnel URL=https://... [PID=<pid>] - cache an already-running tunnel URL for run-tunnel"
	@echo "  make stop-tunnel - stop the cached ngrok tunnel and remove tmp/ngrok state"
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
	@echo "  make fly-config-validate - validate fly.toml with flyctl"
	@echo "  make secrets-scan - scan repository for leaked secrets (gitleaks)"
	@echo "  make msg-gallery - generate Telegram message gallery HTML"
	@echo "  make msg-gallery-md - generate Telegram message gallery Markdown"
	@echo "  make msg-gallery-tg CHAT_ID=<id> - send Telegram message gallery directly to a chat"
	@echo "  make seed CONFIRM=seed - download the latest backup and load it into local Redis"
	@echo "  make check    - fmt + test + build"
	@echo "  make ci-check - run the full local equivalent of CI checks"
	@echo "  make prod-logs CONFIRM=prod-logs - show recent production Fly logs"
	@echo "  make prod-redis-proxy CONFIRM=prod-redis-proxy - open an interactive production Fly Redis proxy"

run:
	@if [ ! -f .env.dev ]; then \
		echo ".env.dev is required. Copy .env.dev.example to .env.dev and fill in the secrets."; \
		exit 1; \
	fi
	@set -a; \
	. ./.env.dev; \
	set +a; \
	IMSUB_REDIS_URL=redis://default:@redis:6379/0 \
	IMSUB_TELEGRAM_WEBHOOK_SECRET= \
	$(GO) run ./cmd/imsub

run-tunnel:
	@if [ ! -f .env.dev ]; then \
		echo ".env.dev is required. Copy .env.dev.example to .env.dev and fill in the secrets."; \
		exit 1; \
	fi
	@set -a; \
	. ./.env.dev; \
	set +a; \
	public_base_url="$(PUBLIC_BASE_URL)"; \
	if [ -z "$$IMSUB_TELEGRAM_WEBHOOK_SECRET" ]; then \
		echo "IMSUB_TELEGRAM_WEBHOOK_SECRET must be set in .env.dev for tunnel mode"; \
		exit 1; \
	fi; \
	if [ -n "$$public_base_url" ]; then \
		mkdir -p tmp; \
		printf '%s\n' "$$public_base_url" >"$(TUNNEL_URL_FILE)"; \
	fi; \
	if [ -z "$$public_base_url" ]; then \
		if [ -s "$(TUNNEL_PID_FILE)" ]; then \
			pid="$$(cat "$(TUNNEL_PID_FILE)")"; \
			if ! kill -0 "$$pid" >/dev/null 2>&1; then \
				rm -f "$(TUNNEL_PID_FILE)" "$(TUNNEL_URL_FILE)"; \
			fi; \
		fi; \
		if [ ! -s "$(TUNNEL_URL_FILE)" ]; then \
			$(MAKE) tunnel; \
		fi; \
		if [ -s "$(TUNNEL_URL_FILE)" ]; then \
			public_base_url="$$(cat "$(TUNNEL_URL_FILE)")"; \
		fi; \
	fi; \
	if [ -z "$$public_base_url" ]; then \
		public_base_url="$$IMSUB_PUBLIC_BASE_URL"; \
	fi; \
	if [ -z "$$public_base_url" ]; then \
		echo "PUBLIC_BASE_URL could not be resolved from the tunnel cache or .env.dev"; \
		exit 1; \
	fi; \
	echo "using tunnel url $$public_base_url"; \
	IMSUB_PUBLIC_BASE_URL="$$public_base_url" \
	IMSUB_REDIS_URL=redis://default:@redis:6379/0 \
	$(GO) run ./cmd/imsub

tunnel:
	@mkdir -p tmp
	@if [ -s "$(TUNNEL_PID_FILE)" ]; then \
		pid="$$(cat "$(TUNNEL_PID_FILE)")"; \
		if kill -0 "$$pid" >/dev/null 2>&1 && [ -s "$(TUNNEL_URL_FILE)" ]; then \
			echo "ngrok already running at $$(cat "$(TUNNEL_URL_FILE)")"; \
			exit 0; \
		fi; \
	fi; \
	public_base_url="$(PUBLIC_BASE_URL)"; \
	if [ -z "$$public_base_url" ] && [ -f .env.dev ]; then \
		set -a; . ./.env.dev; set +a; \
		public_base_url="$$IMSUB_PUBLIC_BASE_URL"; \
	fi; \
	ngrok_domain=""; \
	if [[ "$$public_base_url" =~ ^https://([^/]+)\.ngrok(-free)?\.(app|dev)($$|/) ]]; then \
		ngrok_domain="$${BASH_REMATCH[1]}.ngrok$${BASH_REMATCH[2]}.$${BASH_REMATCH[3]}"; \
	fi; \
	rm -f "$(TUNNEL_PID_FILE)" "$(TUNNEL_URL_FILE)" "$(TUNNEL_LOG_FILE)"; \
	for attempt in 1 2; do \
		rm -f "$(TUNNEL_URL_FILE).tmp"; \
		if [ -n "$$ngrok_domain" ]; then \
			nohup ngrok http --domain="$$ngrok_domain" --log=stdout 8080 >"$(TUNNEL_LOG_FILE)" 2>&1 & \
		else \
			nohup ngrok http --log=stdout 8080 >"$(TUNNEL_LOG_FILE)" 2>&1 & \
		fi; \
		pid="$$!"; \
		echo "$$pid" >"$(TUNNEL_PID_FILE)"; \
		for _ in $$(seq 1 50); do \
			if python3 -c 'import json, urllib.request; data = json.load(urllib.request.urlopen("$(NGROK_API_URL)", timeout=2)); print(next((t.get("public_url", "") for t in data.get("tunnels", []) if t.get("public_url", "").startswith("https://")), ""))' >"$(TUNNEL_URL_FILE).tmp" 2>/dev/null; then \
				if grep -Eq '^https://[^[:space:]]+$$' "$(TUNNEL_URL_FILE).tmp"; then \
					mv "$(TUNNEL_URL_FILE).tmp" "$(TUNNEL_URL_FILE)"; \
					url="$$(cat "$(TUNNEL_URL_FILE)")"; \
					echo "ngrok running at $$url"; \
					exit 0; \
				fi; \
			fi; \
			if ! kill -0 "$$pid" >/dev/null 2>&1; then \
				break; \
			fi; \
			sleep 1; \
		done; \
		if kill -0 "$$pid" >/dev/null 2>&1; then \
			kill "$$pid" >/dev/null 2>&1 || true; \
			for _ in $$(seq 1 10); do \
				if ! kill -0 "$$pid" >/dev/null 2>&1; then \
					break; \
				fi; \
				sleep 1; \
			done; \
		fi; \
		rm -f "$(TUNNEL_PID_FILE)" "$(TUNNEL_URL_FILE).tmp"; \
		if [ "$$attempt" = "1" ]; then \
			sleep 2; \
		fi; \
	done; \
	rm -f "$(TUNNEL_PID_FILE)" "$(TUNNEL_URL_FILE)" "$(TUNNEL_URL_FILE).tmp"; \
	echo "ngrok exited early or timed out; see $(TUNNEL_LOG_FILE)" >&2; \
	exit 1

attach-tunnel:
	@if [ -z "$(URL)" ]; then \
		echo "URL=https://... is required"; \
		exit 1; \
	fi
	@mkdir -p tmp
	@printf '%s\n' "$(URL)" >"$(TUNNEL_URL_FILE)"
	@if [ -n "$(PID)" ]; then \
		printf '%s\n' "$(PID)" >"$(TUNNEL_PID_FILE)"; \
	fi
	@echo "cached tunnel url $(URL)"

stop-tunnel:
	@if [ -s "$(TUNNEL_PID_FILE)" ]; then \
		pid="$$(cat "$(TUNNEL_PID_FILE)")"; \
		if kill -0 "$$pid" >/dev/null 2>&1; then \
			kill "$$pid" >/dev/null 2>&1 || true; \
			for _ in $$(seq 1 10); do \
				if ! kill -0 "$$pid" >/dev/null 2>&1; then \
					break; \
				fi; \
				sleep 1; \
			done; \
		fi; \
	fi
	@rm -f "$(TUNNEL_PID_FILE)" "$(TUNNEL_URL_FILE)" "$(TUNNEL_URL_FILE).tmp" "$(TUNNEL_LOG_FILE)"

seed:
	@if [ "$(CONFIRM)" != "seed" ]; then \
		echo "refusing to seed without CONFIRM=seed"; \
		exit 1; \
	fi
	@mkdir -p tmp/backups
	$(GO) run ./cmd/imsub-admin backup-download -env .env -out tmp/backups/latest.jsonl.gz
	$(GO) run ./cmd/imsub-admin backup-load -from-file tmp/backups/latest.jsonl.gz -redis-url redis://default:@redis:6379/0 -confirm=backup-load

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

fly-config-validate:
	flyctl config validate --strict -c fly.toml

secrets-scan:
	$(GITLEAKS) detect --no-banner --redact --source=.

msg-gallery:
	$(GO) run ./cmd/imsub-message-gallery --out ./imsub-message-gallery.html

msg-gallery-md:
	$(GO) run ./cmd/imsub-message-gallery --format md --out ./imsub-message-gallery.md

msg-gallery-tg:
	$(GO) run ./cmd/imsub-message-gallery --format telegram --chat-id $(CHAT_ID)

check: fmt test build

ci-check: fmt-check vet build test test-integration lint vuln secrets-scan

prod-logs:
	@if [ "$(CONFIRM)" != "prod-logs" ]; then \
		echo "refusing to read production logs without CONFIRM=prod-logs"; \
		exit 1; \
	fi
	flyctl logs -a $(APP) --no-tail

prod-redis-proxy:
	@if [ "$(CONFIRM)" != "prod-redis-proxy" ]; then \
		echo "refusing to open the production Redis proxy without CONFIRM=prod-redis-proxy"; \
		exit 1; \
	fi
	flyctl redis proxy
