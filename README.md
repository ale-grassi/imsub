# ImSub

[![CI](https://github.com/ale-grassi/imsub/actions/workflows/ci.yml/badge.svg)](https://github.com/ale-grassi/imsub/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/ale-grassi/imsub)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Gate your Telegram groups with Twitch subscriptions — automatically.**

ImSub is a Telegram bot that manages access to private Telegram groups based on active Twitch subscriptions. Viewers link their Twitch account and get one-click invite links to groups they're subscribed to. Creators link their Twitch channel, bind a Telegram group, and the bot handles join and revoke from there — no manual moderation needed.

Access is enforced continuously: Twitch EventSub webhooks trigger grants and kicks within seconds, and a background reconciler re-syncs every 15 minutes as a safety net. The whole system is a single Go binary backed by Redis, with no external queues or workers.

---

## Table of Contents

- [Features](#features)
- [How It Works](#how-it-works)
- [Quick Start](#quick-start)
- [Bot Commands](#bot-commands)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Local Development](#local-development)
- [Observability](#observability)
- [Security](#security)
- [Architecture](#architecture)
- [Next Steps](#next-steps)
- [Contributing](#contributing)
- [License](#license)

---

## Features

- **Real-time access control** — EventSub webhooks grant and revoke group access within seconds of subscription changes.
- **Immediate invite delivery** — when a `channel.subscribe` EventSub arrives for a linked viewer, ImSub can proactively DM fresh Telegram join links without waiting for `/start`.
- **Background reconciliation** — a periodic job re-syncs every active creator's subscriber list every 15 minutes as a safety net.
- **Creator blocklist sync** — creators can sync permanent Twitch bans into ImSub so blocked users stop receiving invites, have join requests declined, and are removed from managed groups.
- **OAuth-based linking** — both viewers and creators securely link their Twitch accounts through standard OAuth flows.
- **Self-contained** — single Go binary with Redis; no message queues, external workers, or additional databases.
- **Prometheus metrics** — built-in `/metrics` endpoint for monitoring with Grafana or any Prometheus-compatible stack.
- **Internationalization** — localized bot messages (currently English and Italian).
- **Production-ready** — multi-stage Docker build, Fly.io deployment config, health checks, rate limiting, and security headers included.

---

## How It Works

1. A **Creator** links their Twitch channel via `/creator` and links a Telegram group with `/linkgroup`.
2. ImSub subscribes to Twitch EventSub events (`channel.subscribe`, `channel.subscription.end`) for that channel.
3. A **Viewer** runs `/start`, links their Twitch account, and sees join buttons for any group where they have an active subscription.
4. When a subscription ends, the bot automatically kicks the viewer from the group and notifies them with a resubscribe link.
5. Every 15 minutes, a reconciler re-syncs subscriptions to catch any missed events.

---

## Quick Start

### Prerequisites

- **Go 1.26+**
- **Redis** — reachable from the machine running ImSub
- **Telegram Bot Token** — create one via [@BotFather](https://t.me/BotFather)
- **Twitch OAuth Credentials** — register an application in the [Twitch Developer Console](https://dev.twitch.tv/console/apps)
- **Public HTTPS URL** — reachable by Twitch for OAuth callbacks and EventSub webhooks

### Setup

1. Clone the repository:

   ```bash
   git clone https://github.com/ale-grassi/imsub.git
   cd imsub
   ```

2. Copy the example environment file and fill in the required values (see [Configuration](#configuration)):

   ```bash
   cp .env.example .env
   ```

3. In the [Twitch Developer Console](https://dev.twitch.tv/console/apps), set the OAuth redirect URI to:

   ```
   <IMSUB_PUBLIC_BASE_URL>/auth/callback
   ```

   For example: `https://imsub.fly.dev/auth/callback`

4. Ensure `IMSUB_PUBLIC_BASE_URL` points to a public HTTPS endpoint that Twitch can reach for:

   - OAuth callback: `/auth/callback`
   - EventSub webhooks: `IMSUB_TWITCH_WEBHOOK_PATH` (default `/webhooks/twitch`)

5. Run the bot:

   ```bash
   go run ./cmd/imsub
   ```

   If `IMSUB_TELEGRAM_WEBHOOK_SECRET` is set, ImSub registers a Telegram webhook at `IMSUB_TELEGRAM_WEBHOOK_PATH`. If it is unset, ImSub uses Telegram long polling instead.

For production deployments, see [Deployment](#deployment).

---

## Bot Commands

| Command | Context | Description |
|---------|---------|-------------|
| `/start` | Private chat | Link a Twitch account and see available groups |
| `/creator` | Private chat | Link a Twitch creator account or view creator status |
| `/reset` | Private chat | Guided deletion of viewer data, creator data, or both |
| `/help` | Private chat | Help desk flow for reports and troubleshooting, with one practical LLM-generated answer per user each week |
| `/info` | Private chat | About screen with how it works, commands, and project link |
| `/linkgroup` | Group chat | Link the current group to your creator account (admin only) |
| `/unlinkgroup` | Group chat | Unlink the current group from your creator account (owner only) |

---

## Configuration

All configuration is done through environment variables. See `.env.example` for the full template.

### Required

| Variable | Description |
|----------|-------------|
| `IMSUB_TELEGRAM_BOT_TOKEN` | Telegram bot token from @BotFather |
| `IMSUB_TELEGRAM_MTPROTO_API_ID` | Telegram MTProto application ID for the operator-managed user session |
| `IMSUB_TELEGRAM_MTPROTO_API_HASH` | Telegram MTProto application hash for the operator-managed user session |
| `IMSUB_TELEGRAM_MTPROTO_SESSION` | Base64-encoded serialized MTProto session for the operator-managed user account |
| `IMSUB_TWITCH_CLIENT_ID` | Twitch application client ID |
| `IMSUB_TWITCH_CLIENT_SECRET` | Twitch application client secret |
| `IMSUB_TWITCH_EVENTSUB_SECRET` | Shared secret for Twitch EventSub HMAC verification |
| `IMSUB_PUBLIC_BASE_URL` | Public URL where the bot is reachable (e.g. `https://imsub.fly.dev`) |
| `IMSUB_REDIS_URL` | Redis connection URL (e.g. `rediss://default:pw@host:port`) |

### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `IMSUB_TELEGRAM_WEBHOOK_SECRET` | — | Secret for Telegram webhook validation |
| `IMSUB_TELEGRAM_WEBHOOK_PATH` | `/webhooks/telegram` | Telegram webhook endpoint path |
| `IMSUB_TWITCH_WEBHOOK_PATH` | `/webhooks/twitch` | Twitch EventSub webhook endpoint path |
| `IMSUB_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `IMSUB_DEBUG_LOGS` | `false` | Enable debug logging (`true`, `1`, `yes`, `on`, `debug`) |
| `IMSUB_METRICS_ENABLED` | `true` | Enable Prometheus metrics endpoint |
| `IMSUB_METRICS_PATH` | `/metrics` | Path for the metrics endpoint |
| `IMSUB_S3_ENDPOINT` | — | S3-compatible endpoint for periodic Redis backups, such as `fly.storage.tigris.dev` |
| `IMSUB_S3_BUCKET` | — | Dedicated Tigris/S3 bucket used only for backup objects |
| `IMSUB_S3_ACCESS_KEY_ID` | — | Access key for the backup bucket |
| `IMSUB_S3_SECRET_ACCESS_KEY` | — | Secret key for the backup bucket |
| `IMSUB_S3_REGION` | `auto` | Region passed to the S3-compatible client |
| `IMSUB_BACKUP_INTERVAL` | `6h` | Periodic Redis backup cadence when S3 backup config is set |

---

When any backup variable is set, the full backup configuration must be present or startup fails. The backup bucket should be dedicated to ImSub backups, and object retention should be handled with a bucket lifecycle rule in Tigris rather than by the app.

## Deployment

ImSub ships with everything needed for [Fly.io](https://fly.io) deployment.

### Included Files

- `Dockerfile` — multi-stage build producing a minimal Alpine image
- `fly.toml` — HTTP service configuration, health checks, and metrics scraping

### GitHub Actions Deploy Flow

Pushes to `main` run the `Main` GitHub Actions workflow. Deployment happens only after Fly config validation, checks, linting, vulnerability scanning, and secret scanning pass. There is no local `make deploy` target.

### Production Utility Commands

These commands are intentionally prefixed with `prod-` and require an exact confirmation string:

```bash
make prod-logs CONFIRM=prod-logs
make prod-redis-proxy CONFIRM=prod-redis-proxy
```

### Health Check

- **Endpoint:** `GET /healthz` (includes Redis connectivity check)
- **Service check:** Fly polls `/healthz` every 30 seconds
- **Machine check:** Verifies `/healthz` before completing a rollout

---

## Local Development

### Prerequisites

- Go 1.26+
- `pre-commit` (optional, for local secret scanning hook)

### Getting Started

```bash
# Open the repository in the devcontainer first.

# Copy the local development environment file
cp .env.dev.example .env.dev

# Fill in the dev bot token and Twitch credentials in .env.dev

# Run format + test + build
make check

# Start the bot in long polling mode
make run
```

For webhook tunnel mode, use the same `.env.dev` file and run `make run-tunnel`.

### Devcontainer

The repository includes a local-build devcontainer image. Rebuilding the container builds [`.devcontainer/Dockerfile`](/workspace/.devcontainer/Dockerfile), which bakes in the stable toolchain layers: Go, `gopls`, `dlv`, Node.js, `flyctl`, `golangci-lint`, `govulncheck`, `gitleaks`, `pre-commit`, `actionlint`, `xdg-open`, `twitch`, `ngrok`, and the AI coding CLIs (`codex`, `claude`, `gemini`). Host directories for CLI state (`~/.claude`, `~/.fly`, `~/.codex`, `~/.gemini`, `~/.config/ngrok`) are bind-mounted so login state persists across rebuilds. The `initializeCommand` creates placeholder host paths if missing.

The local Redis service lives at `redis://default:@redis:6379/0` from inside the devcontainer and keeps its data in a named Docker volume so seeded data survives rebuilds and restarts.

The post-create step is intentionally small now. It only applies workspace-specific setup such as Git config inclusion, commit-signing integration, `pre-commit install`, and `go mod download`.

**1Password SSH signing:** If you require signed commits, the host must provide `/opt/1Password/op-ssh-sign` and the relevant Git signer configuration. Without that signer, signed commits in the container will not work.

### Make Targets

| Target | Description |
|--------|-------------|
| `make fmt` | Format Go source files |
| `make test` | Run unit tests with race detector |
| `make test-integration` | Run integration-tagged tests (`IMSUB_TEST_REDIS_URL=redis://localhost:6379/0 make test-integration` enables the real Redis backup round-trip test) |
| `make build` | Build all packages |
| `make lint` | Run golangci-lint |
| `make cover` | Generate `coverage.out` and `coverage.html` |
| `make cover-open` | Open interactive coverage report in browser |
| `make vuln` | Run govulncheck against all packages |
| `make secrets-scan` | Scan for leaked secrets with gitleaks |
| `make run` | Run ImSub locally in long polling mode using `.env.dev` and the devcontainer Redis |
| `make run-tunnel [PUBLIC_BASE_URL=https://...]` | Run ImSub in local webhook mode using `.env.dev` and the devcontainer Redis |
| `make tunnel` | Start or reuse a cached ngrok HTTPS tunnel for `http://localhost:8080` |
| `make attach-tunnel URL=https://... [PID=<pid>]` | Cache an already-running tunnel URL for `make run-tunnel` |
| `make stop-tunnel` | Stop the cached ngrok tunnel and remove its temp files |
| `make seed CONFIRM=seed` | Download the latest production backup and load it into the local devcontainer Redis |
| `make prod-logs CONFIRM=prod-logs` | Show recent production Fly logs |
| `make prod-redis-proxy CONFIRM=prod-redis-proxy` | Open an interactive production Fly Redis proxy |
| `make check` | Run `fmt` + `test` + `build` |
| `make ci-check` | Run the full local equivalent of CI checks |

### Local Webhook Testing

Use `.env.dev` and `make run` for the fast local loop. Telegram stays in long polling mode, while Twitch EventSub requests can be generated directly against the local HTTP server.

Verify the webhook challenge handler:

```bash
set -a; . ./.env.dev; set +a
twitch event verify-subscription subscribe \
  -F http://localhost:8080/webhooks/twitch \
  -s "$IMSUB_TWITCH_EVENTSUB_SECRET"
```

Send synthetic EventSub notifications to the local webhook:

```bash
set -a; . ./.env.dev; set +a
twitch event trigger subscribe \
  -F http://localhost:8080/webhooks/twitch \
  -s "$IMSUB_TWITCH_EVENTSUB_SECRET"
twitch event trigger unsubscribe \
  -F http://localhost:8080/webhooks/twitch \
  -s "$IMSUB_TWITCH_EVENTSUB_SECRET"
twitch event trigger ban \
  -F http://localhost:8080/webhooks/twitch \
  -s "$IMSUB_TWITCH_EVENTSUB_SECRET"
twitch event trigger unban \
  -F http://localhost:8080/webhooks/twitch \
  -s "$IMSUB_TWITCH_EVENTSUB_SECRET"
```

### Tunnel Testing

Use the same `.env.dev` file when you need the local app reachable from Twitch and Telegram over public HTTPS.

```bash
# Set IMSUB_TELEGRAM_WEBHOOK_SECRET in .env.dev first.
# Authenticate ngrok once inside the devcontainer if needed.
ngrok config add-authtoken <your-token>

make run-tunnel
```

`make run-tunnel` reuses the cached ngrok URL from `tmp/ngrok-url` when available. If no live tunnel is cached, it starts one automatically, stores its URL and PID under `tmp/`, and uses that public URL for Telegram webhooks and Twitch callbacks.

If your ngrok account has an assigned stable domain, set `IMSUB_PUBLIC_BASE_URL` in `.env.dev` to that `https://...ngrok...` value. `make tunnel` will bind ngrok to the same host so Twitch and Telegram callback URLs stay stable across restarts.

If you started `ngrok` manually outside `make`, adopt it with:

```bash
make attach-tunnel URL=https://<your-ngrok-url> PID=<optional-pid>
make run-tunnel
```

Passing `PUBLIC_BASE_URL=https://...` to `make run-tunnel` also updates the cached tunnel URL for future runs.

### Download or Load a Backup

ImSub includes operator-only backup commands that read Tigris credentials directly from `.env` via `godotenv`.

Download the latest available backup without touching Redis:

```bash
go run ./cmd/imsub-admin backup-download -env .env -out tmp/backups/latest.jsonl.gz
```

Download a specific object key:

```bash
go run ./cmd/imsub-admin backup-download -env .env -key backups/imsub-2026-03-12T12-00-00Z.jsonl.gz -out tmp/backups/imsub-2026-03-12T12-00-00Z.jsonl.gz
```

Load a downloaded backup into the local Redis:

```bash
go run ./cmd/imsub-admin backup-load -from-file tmp/backups/latest.jsonl.gz -redis-url redis://default:@redis:6379/0 -confirm=backup-load
```

Load directly from object storage into the configured Redis:

```bash
go run ./cmd/imsub-admin backup-load -env .env -key backups/imsub-2026-03-12T12-00-00Z.jsonl.gz -confirm=backup-load
```

For the common local flow, use:

```bash
make seed CONFIRM=seed
```

The backup-load flow uses `RESTORE REPLACE` for each `imsub:*` key, so it keeps the explicit confirmation requirement. The backup-download flow is read-only and does not require confirmation.

### Pre-commit Hooks

```bash
pre-commit run --all-files
```

In the devcontainer, `pre-commit` is installed in the image and the git hook is registered automatically by the post-create step. The hooks are intentionally close to CI: `make fmt-check`, `make vet`, `make build`, `make lint`, `make test`, `make test-integration` against the devcontainer Redis, `make vuln`, `actionlint`, `make fly-config-validate`, and `make secrets-scan`.

---

## Observability

### Logging

- **Library:** Go `log/slog` (JSON format)
- **Levels:** `INFO` by default, `DEBUG` when `IMSUB_DEBUG_LOGS=true`
- **Per-request access logs** include `request_id`, `method`, `route`, `status`, `duration_ms`, `client_ip`, `bytes`

### Metrics

Prometheus metrics are exposed at `GET /metrics` when `IMSUB_METRICS_ENABLED=true` (default). The path is configurable via `IMSUB_METRICS_PATH`.

Key metrics include:

| Metric | Type | Description |
|--------|------|-------------|
| `imsub_http_requests_total` | Counter | Total HTTP requests |
| `imsub_http_request_duration_seconds` | Histogram | HTTP request latency |
| `imsub_http_requests_in_flight` | Gauge | Currently active HTTP requests |
| `imsub_oauth_callbacks_total` | Counter | OAuth callback invocations |
| `imsub_eventsub_messages_total` | Counter | EventSub messages processed |
| `imsub_telegram_webhook_updates_total` | Counter | Telegram webhook updates received |
| `imsub_telegram_daily_active_users` | Gauge | Rolling 24h unique Telegram users with direct bot activity |
| `imsub_linked_viewer_accounts` | Gauge | Current linked viewer accounts |
| `imsub_linked_creator_accounts` | Gauge | Current linked creator accounts |
| `imsub_managed_groups` | Gauge | Current managed Telegram groups |
| `imsub_background_jobs_total` | Counter | Background job executions |
| `imsub_background_job_duration_seconds` | Histogram | Background job latency |
| `imsub_creator_token_refresh_total` | Counter | Creator token refresh attempts |
| `imsub_creator_auth_state_transitions_total` | Counter | Creator auth state transitions |
| `imsub_creators_reconnect_required` | Gauge | Creators currently marked for reconnect |
| `imsub_reset_executions_total` | Counter | Reset executions by scope and result |
| `imsub_group_registrations_total` | Counter | Group registration attempts |
| `imsub_group_unregistrations_total` | Counter | Group unregistration attempts |
| `imsub_viewer_oauth_total` | Counter | Viewer OAuth completion results |
| `imsub_creator_oauth_total` | Counter | Creator OAuth completion results |
| `imsub_viewer_access_total` | Counter | Viewer access workflow results |
| `imsub_reconciliation_repairs_total` | Counter | Integrity and repair counts |
| `imsub_telegram_commands_total` | Counter | Telegram slash command usage by command and chat type |
| `imsub_telegram_command_response_duration_seconds` | Histogram | Telegram command latency to first successful bot response, or handler error before one is sent |
| `imsub_telegram_api_errors_total` | Counter | Telegram API call failures by method and normalized reason |
| `imsub_telegram_kick_actions_total` | Counter | Telegram kick actions by reason and result |
| `imsub_telegram_mtproto_bootstrap_total` | Counter | Initial MTProto bootstrap sync attempts by outcome |

Fly.io's managed Prometheus can scrape this endpoint for Grafana dashboards.

The canonical committed dashboard is:

- `grafana/imsub-canonical-dashboard.json` for product, Telegram, application health, Fly runtime, and log context in one board

---

## Security

### Implemented Protections

- **EventSub HMAC verification** — validates Twitch webhook signatures
- **Replay protection** — timestamp tolerance + Redis-based deduplication (24h TTL)
- **Telegram webhook secret** — validates webhook requests when using webhook mode
- **HTTP rate limiting** — fixed-window rate limiter on sensitive endpoints
- **Security headers** — baseline security headers middleware

### Operational Requirements

The bot must have sufficient permissions in creator-linked groups for invite link generation and kick/unban operations.

---

## Architecture

### Project Layout

The executable entrypoint is `cmd/imsub/main.go`. All internal packages are under `internal/`:

```
internal/
  app/             → process startup and top-level dependency wiring
  core/            → unified domain entities, use-case orchestration, and service contracts
  usecase/         → transport-facing workflows built on core services
  jobs/            → background schedulers and periodic runtime jobs
  events/          → shared event model used across use cases and workflows
  operator/        → operator-facing read models projected from shared events
  adapter/
    redis/         → persistence adapter
    twitch/        → Twitch API/EventSub adapter
  transport/
    http/          → server, middleware, controllers, pages
    telegram/      → Telegram client helpers, bot handlers, group ops, UI helpers
  platform/
    config/        → env loading + validation
    observability/ → metrics + HTTP observability middleware
    i18n/          → localization catalogs + translation utilities
    httputil/      → shared HTTP utilities (request ID, client IP, status recording)
    ratelimit/     → keyed rate-limit coordination
```

**In short:** `app` boots the process, `core` holds all business logic and storage interfaces, `usecase` coordinates transport-facing workflows, `jobs` handles periodic reconciliation, `transport` manages inputs and outputs (Telegram flows and HTTP webhooks), and `adapter` connects the interfaces to Redis and Twitch.

### Request Model

The bot is mostly state-driven. User actions from Telegram callbacks/commands read current state from Redis, compute the next valid state transition, and then render the corresponding UI update. External signals from Twitch EventSub also mutate the same state model, keeping user-facing behavior consistent even if events arrive out of order or users retry actions.

### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Redirects to the GitHub repository homepage |
| `GET` | `/healthz` | Liveness/readiness check (includes Redis ping) |
| `GET` | `/metrics` | Prometheus metrics when `IMSUB_METRICS_ENABLED=true` |
| `GET` | `/auth/start/{state}` | Validates OAuth state, serves Twitch auth launch page |
| `GET` | `/auth/callback` | Completes viewer or creator OAuth based on stored state |
| `POST` | `/webhooks/twitch` | EventSub verification + notification intake |
| `POST` | `/webhooks/telegram` | Telegram webhook intake when `IMSUB_TELEGRAM_WEBHOOK_SECRET` is configured |

### Redis Data Model

| Key | Type | Purpose |
|-----|------|---------|
| `imsub:oauth:{state}` | string (JSON) | OAuth state payload (viewer/creator mode, user id, lang, prompt message id) |
| `imsub:eventmsg:{message_id}` | string | EventSub idempotency key |
| `imsub:user:{telegram_user_id}` | hash | Viewer identity (twitch_user_id, twitch_login, language, verified_at) |
| `imsub:users` | set | All Telegram user IDs with a linked identity |
| `imsub:twitch_to_tg:{twitch_user_id}` | string | Reverse mapping: Twitch user → Telegram user |
| `imsub:creator:{creator_id}` | hash | Creator profile and OAuth tokens |
| `imsub:creators` | set | All creator IDs |
| `imsub:creator:subscribers:{creator_id}` | set | Twitch user IDs subscribed to this creator (refreshed every 15m) |
| `imsub:creator:by_owner:{telegram_user_id}` | set | Creator IDs owned by this Telegram user |
| `imsub:group:{chat_id}` | hash | Managed Telegram group metadata |
| `imsub:groups` | set | All managed group chat IDs |
| `imsub:groups:by_creator:{creator_id}` | set | Managed groups owned by a creator |
| `imsub:group:tracked:{chat_id}` | set | Telegram user IDs tracked for that group |
| `imsub:group:untracked:{chat_id}` | set | Telegram user IDs seen in the group but not yet tracked |
| `imsub:user:groups:tracked:{telegram_user_id}` | set | Reverse index: tracked group chat IDs for this Telegram user |
| `imsub:schema_version` | string | Data model version marker |

> **Note:** Group membership is group-centric. The canonical tracked set is `imsub:group:tracked:{chat_id}`, with `imsub:user:groups:tracked:{telegram_user_id}` kept as its reverse index.

**Active vs. inactive creator:** A creator is "active" when it owns at least one managed Telegram group, meaning the creator ran `/linkgroup` successfully. An "inactive" creator has linked their Twitch account via `/creator` but has not yet linked any group. Most flows (join buttons, kicks, reconciler, reset scans) only operate on active creators.

### Detailed Flows

<details>
<summary><strong>Viewer Flow</strong></summary>

1. Viewer runs `/start`.
2. Bot checks if viewer identity exists in Redis.
3. If not linked:
   - Creates OAuth state (`mode=viewer`, TTL 10m)
   - Sends link button + fallback URL
4. OAuth callback:
   - Exchanges code for token
   - Fetches Twitch user
   - Stores viewer identity
   - Deletes prior prompt message (if tracked)
   - Rebuilds join eligibility
5. Eligibility and buttons:
   - Iterates all active creators
   - Checks subscription cache
   - For each subscribed creator, iterates that creator's managed groups
   - If already in a group → skips that group
   - If not in a group → generates invite link + shows join button
   - If no longer subscribed → removes tracked membership for each managed group

</details>

<details>
<summary><strong>Creator Flow</strong></summary>

1. Creator runs `/creator`.
2. If a creator record owned by this Telegram user exists:
   - Sends creator status (EventSub state + subscriber count placeholder)
3. If not linked:
   - Creates OAuth state (`mode=creator`, TTL 10m)
   - Sends creator auth button
4. OAuth callback:
   - Exchanges code for token
   - Verifies required scope `channel:read:subscriptions`
   - Stores creator record + tokens
   - EventSub and subscriber dump are deferred until `/linkgroup`

</details>

<details>
<summary><strong>Group Registration Flow</strong></summary>

`/linkgroup` checks:

1. Must be in group chat.
2. Caller must be admin/creator in that group.
3. Caller must have a creator account linked.

If valid, bot stores a managed-group record keyed by `chat_id` and initializes:

- EventSub subscriptions (`channel.subscribe`, `channel.subscription.end`)
- Initial subscriber dump in background

> **Note:** A single Telegram user can only link one Twitch creator account, but a creator may own multiple managed groups.

</details>

<details>
<summary><strong>Reset Flow</strong></summary>

`/reset` is role-aware and supports deleting viewer data, creator data, or both.

**Viewer reset:**
- Reads tracked group IDs directly from Redis
- Kicks/unbans the user from those groups (best-effort)
- Removes the user's tracked-group links
- Ignores groups where the user was only observed as untracked
- Deletes viewer identity and Twitch mapping

**Creator reset:**
- Deletes owned creator records, managed groups, and subscriber cache
- Removes creator from all indices

</details>

<details>
<summary><strong>EventSub Behavior</strong></summary>

**Startup bootstrap:**
- Waits ~3 seconds after boot
- Loads creators, verifies required EventSub types, repairs missing subscriptions

**Webhook processing:**
- Verifies Twitch HMAC signature
- Enforces message freshness (±10 minutes)
- Deduplicates by message ID in Redis (24h TTL)
- `channel.subscribe` → adds to subscription cache and can proactively DM fresh invites to linked viewers
- `channel.subscription.end` → removes from cache, kicks from group, notifies user

</details>

### Known Constraints

- OAuth state TTL is 10 minutes.
- EventSub bootstrap runs shortly after startup; if Redis has no creators, no verification happens.
- Localization currently supports only English and Italian.

---

## Next Steps

Planned improvements and open design questions, roughly ordered by impact.

### Operational visibility

- **Telegram operator log channel**: forward high-signal service events to one Telegram channel owned by the operator, such as EventSub failures, OAuth errors, reconciliation warnings, failed kicks, and automatic unregisters. This is for bot/service monitoring, not creator-facing audit logs.
- **Creator moderation log channel**: let each creator optionally bind one Telegram channel where ImSub posts member-access actions for that creator's groups, such as approved joins, declined joins, kicks, grace-expiry removals, and ban-sync removals, with the affected user and reason.
- **Dashboard refinement**: keep expanding Grafana panels and alerting around product health, Telegram API errors, reconciliation drift, and creator reconnect state so operator action is obvious without reading logs.

### Subscription lifecycle

- **EventSub secret rotation key-ring**: replace single static `IMSUB_TWITCH_EVENTSUB_SECRET` usage with a shared persisted key-ring (`current` + `previous`) so all app instances verify with dual-secret during a bounded grace period. Rotate on schedule (not every restart), explicitly migrate EventSub subscriptions to the new secret (create/verify/delete old), and retire the previous key after migration completes.
- **Subscription tier awareness**: different tiers could map to different groups or roles within the same group (e.g. Tier 3 gets a VIP group).

### Multi-entity support

- **Other platforms integration**: support other platforms (like YouTube and Patreon) alongside Twitch to gather subscriptions from multiple sources.
- **Multiple Twitch accounts per viewer**: let a Telegram user link more than one Twitch account and merge subscription eligibility across them.
- **Multiple creator accounts per owner**: let a single Telegram user own more than one creator record.

### Access control

- **Creator allowlist**: let creators manually grant group access to specific users (e.g. mods, friends) who aren't subscribers, bypassing the subscription check.
- **Sub-only channel mode**: let a creator flag a Telegram channel as sub-only so that only verified subscribers can view its posts. The bot would manage channel membership the same way it manages group membership — granting access on `channel.subscribe`, revoking on `channel.subscription.end`, and reconciling periodically. This extends the existing group flow to Telegram channels, which have different invite-link and kick semantics.

### GDPR compliance

- **Privacy surface polish**: surface the privacy policy link in `/help` and `/info`, not just the onboarding flows, and keep the `/reset` export/delete copy aligned with the final policy wording.

### Support and info

- **Localization**: add more languages beyond English and Italian.
- **Inline status refresh**: let viewers check their subscription status without going through the full `/start` flow again.

---

## Contributing

Contributions are welcome! Here's how to get started:

1. Fork the repository and create a feature branch.
2. Run `make check` (format + test + build) before committing.
3. Ensure that `make lint` passes with no issues.
4. Open a pull request with a clear description of your changes.

CI will automatically run format checks, vet, build, unit tests, integration tests, linting, vulnerability scanning, and secret detection on your PR.

---

## License

This project is licensed under the [MIT License](LICENSE).
