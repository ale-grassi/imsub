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
- [Local Development](#local-development)
- [Deployment](#deployment)
- [Observability](#observability)
- [Security](#security)
- [Architecture](#architecture)
- [Next Steps](#next-steps)
- [Contributing](#contributing)
- [License](#license)

---

## Features

- **Real-time access control** — EventSub webhooks grant and revoke group access within seconds of subscription changes.
- **Background reconciliation** — a periodic job re-syncs every active creator's subscriber list every 15 minutes as a safety net.
- **OAuth-based linking** — both viewers and creators securely link their Twitch accounts through standard OAuth flows.
- **Self-contained** — single Go binary with Redis; no message queues, external workers, or additional databases.
- **Prometheus metrics** — built-in `/metrics` endpoint for monitoring with Grafana or any Prometheus-compatible stack.
- **Internationalization** — localized bot messages (currently English and Italian).
- **Production-ready** — multi-stage Docker build, Fly.io deployment config, health checks, rate limiting, and security headers included.

---

## How It Works

1. A **Creator** links their Twitch channel via `/creator` and binds a Telegram group with `/registergroup`.
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

4. Run the bot:

   ```bash
   go run ./cmd/imsub
   ```

For production deployments, see [Deployment](#deployment).

---

## Bot Commands

| Command | Context | Description |
|---------|---------|-------------|
| `/start` | Private chat | Link a Twitch account and see available groups |
| `/creator` | Private chat | Link a Twitch creator account or view creator status |
| `/reset` | Private chat | Guided deletion of viewer data, creator data, or both |
| `/registergroup` | Group chat | Bind the current group to your creator account (admin only) |

---

## Configuration

All configuration is done through environment variables. See `.env.example` for the full template.

### Required

| Variable | Description |
|----------|-------------|
| `IMSUB_TELEGRAM_BOT_TOKEN` | Telegram bot token from @BotFather |
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

---

## Local Development

### Prerequisites

- Go 1.26+
- Redis reachable from the local process
- `pre-commit` (optional, for local secret scanning hook)

### Getting Started

```bash
# Copy and fill in environment variables
cp .env.example .env

# Run format + test + build
make check

# Start the bot
go run ./cmd/imsub
```

When you open the repository in the devcontainer, the post-create bootstrap installs the CLI tools used by the `Makefile` targets (`flyctl`, `golangci-lint`, `govulncheck`, `gitleaks`, `xdg-open`) and the AI coding CLIs (`codex`, `claude`, `gemini`) so the local workflow works without extra manual setup.

The devcontainer also bind-mounts `~/.codex`, `~/.claude`, `~/.claude.json`, `~/.fly`, and `~/.gemini` from the host into the container so CLI settings and login state persist across rebuilds. Anthropic’s docs note Claude Code uses both `~/.claude/` and `~/.claude.json` for user-level state, so both are mounted for the `claude` CLI. `claude` also supports `ANTHROPIC_API_KEY`, but for interactive usage the normal flow is `/login`. `flyctl` will reuse the host login from `~/.fly`, with `FLY_ACCESS_TOKEN` still supported as an override.

These mounts are tolerant of missing host state for Codex, Claude, Gemini, Fly, SSH, and Git config: the devcontainer `initializeCommand` creates placeholder host directories and files before the container starts. 1Password SSH signing is different: if you require signed commits, the host must provide `/opt/1Password/op-ssh-sign` and the relevant Git signer configuration. Without that signer, signed commits in the container will not work by design.

For Fly Redis inspection from VS Code, use the `fly redis proxy` task or run `make redis-proxy`. Both use the interactive `flyctl redis proxy` flow so Fly can prompt for the managed Redis database, print the local proxy port, and show the generated password. Point your Redis extension at the printed local host/port, typically `127.0.0.1:16379`, and use the password shown by `flyctl`.

### Make Targets

| Target | Description |
|--------|-------------|
| `make fmt` | Format Go source files |
| `make test` | Run unit tests with race detector |
| `make test-integration` | Run integration-tagged tests |
| `make build` | Build all packages |
| `make lint` | Run golangci-lint |
| `make cover` | Generate `coverage.out` and `coverage.html` |
| `make cover-open` | Open interactive coverage report in browser |
| `make vuln` | Run govulncheck against all packages |
| `make secrets-scan` | Scan for leaked secrets with gitleaks |
| `make check` | Run `fmt` + `test` + `build` |

### Pre-commit Hooks

```bash
pre-commit install
pre-commit run --all-files
```

---

## Deployment

ImSub ships with everything needed for [Fly.io](https://fly.io) deployment.

### Included Files

- `Dockerfile` — multi-stage build producing a minimal Alpine image
- `fly.toml` — HTTP service configuration, health checks, and metrics scraping

### Deploy Commands

```bash
make deploy     # Deploy to Fly.io
make status     # Show app status
make logs       # Show recent logs
```

### GitHub Actions Deploy Flow

Push to `main` runs `CI`; if it passes, GitHub Actions deploys to Fly.io.


### Health Check

- **Endpoint:** `GET /healthz` (includes Redis connectivity check)
- **Service check:** Fly polls `/healthz` every 30 seconds
- **Machine check:** Verifies `/healthz` before completing a rollout

---

## Observability

### Logging

- **Library:** Go `log/slog` (JSON format)
- **Levels:** `INFO` by default, `DEBUG` when `IMSUB_DEBUG_LOGS=true`
- **Per-request access logs** include `request_id`, `method`, `route`, `status`, `duration_ms`, `client_ip`, `bytes`

### Metrics

Prometheus metrics are exposed at `GET /metrics` (configurable via `IMSUB_METRICS_PATH`).

| Metric | Type | Description |
|--------|------|-------------|
| `imsub_http_requests_total` | Counter | Total HTTP requests |
| `imsub_http_request_duration_seconds` | Histogram | HTTP request latency |
| `imsub_http_requests_in_flight` | Gauge | Currently active HTTP requests |
| `imsub_oauth_callbacks_total` | Counter | OAuth callback invocations |
| `imsub_eventsub_messages_total` | Counter | EventSub messages processed |
| `imsub_telegram_webhook_updates_total` | Counter | Telegram webhook updates received |
| `imsub_background_jobs_total` | Counter | Background job executions |
| `imsub_background_job_duration_seconds` | Histogram | Background job latency |

Fly.io's managed Prometheus can scrape this endpoint for Grafana dashboards.

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
  jobs/            → background schedulers and periodic runtime jobs
  adapter/
    redis/         → persistence adapter
    twitch/        → Twitch API/EventSub adapter
  transport/
    http/          → server, middleware, controllers, pages
    telegram/      → Telegram client helpers, rate limiter, UI helpers
  platform/
    config/        → env loading + validation
    observability/ → metrics + HTTP observability middleware
    i18n/          → localization catalogs + translation utilities
```

**In short:** `app` boots the process, `core` holds all business logic and storage interfaces, `jobs` handles periodic reconciliation, `transport` manages inputs and outputs (Telegram flows and HTTP webhooks), and `adapter` connects the interfaces to Redis and Twitch.

### Request Model

The bot is mostly state-driven. User actions from Telegram callbacks/commands read current state from Redis, compute the next valid state transition, and then render the corresponding UI update. External signals from Twitch EventSub also mutate the same state model, keeping user-facing behavior consistent even if events arrive out of order or users retry actions.

### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Redirects to the GitHub repository homepage |
| `GET` | `/healthz` | Liveness/readiness check (includes Redis ping) |
| `GET` | `/metrics` | Prometheus metrics (Fly/Grafana scraping) |
| `GET` | `/auth/start/{state}` | Validates OAuth state, serves Twitch auth launch page |
| `GET` | `/auth/callback` | Completes viewer or creator OAuth based on stored state |
| `POST` | `/webhooks/twitch` | EventSub verification + notification intake |
| `POST` | `/webhooks/telegram` | Telegram webhook intake (secret-token protected) |

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

**Active vs. inactive creator:** A creator is "active" when it owns at least one managed Telegram group, meaning the creator ran `/registergroup` successfully. An "inactive" creator has linked their Twitch account via `/creator` but has not yet bound any group. Most flows (join buttons, kicks, reconciler, reset scans) only operate on active creators.

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
   - EventSub and subscriber dump are deferred until `/registergroup`

</details>

<details>
<summary><strong>Group Registration Flow</strong></summary>

`/registergroup` checks:

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
- `channel.subscribe` → adds to subscription cache
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

- **Telegram log channel**: forward key events (kicks, joins, errors, EventSub issues) to a dedicated Telegram channel so developers can monitor without checking server logs.
- **Creator kick log channel**: let a creator optionally bind a log channel where the bot posts whenever someone is kicked or leaves, with reason and timestamp.
- **Product and Telegram metrics**: add Prometheus metrics and Grafana panels for daily active bot users, linked viewer/creator accounts, subscription checks, group registrations, command usage, and kick/access actions so the dashboard answers operational and product questions directly.

### Subscription lifecycle

- **Grace period after subscription end**: instead of kicking immediately on `channel.subscription.end`, keep the user in the group for a configurable window (e.g. 24–72h) and kick only if they haven't resubscribed by then. Requires a delayed job or a scheduled sweep.
- **EventSub secret rotation key-ring**: replace single static `IMSUB_TWITCH_EVENTSUB_SECRET` usage with a shared persisted key-ring (`current` + `previous`) so all app instances verify with dual-secret during a bounded grace period. Rotate on schedule (not every restart), explicitly migrate EventSub subscriptions to the new secret (create/verify/delete old), and retire the previous key after migration completes.
- **Gift sub support**: handle `channel.subscription.gift` EventSub events so gifted subs grant group access the same way direct subs do.
- **Subscription tier awareness**: different tiers could map to different groups or roles within the same group (e.g. Tier 3 gets a VIP group).

### Multi-entity support

- **Other platforms integration**: support other platforms (like YouTube and Patreon) alongside Twitch to gather subscriptions from multiple sources.
- **Multiple Twitch accounts per viewer**: let a Telegram user link more than one Twitch account and merge subscription eligibility across them.
- **Multiple creator accounts per owner**: let a single Telegram user own more than one creator record.

### Access control

- **Creator allowlist**: let creators manually grant group access to specific users (e.g. mods, friends) who aren't subscribers, bypassing the subscription check.
- **Creator blocklist**: let creators permanently deny access to specific users regardless of subscription status.

### Group lifecycle

- **Creator reset group action**: when a creator deletes their creator data, ask what to do with members of their managed groups. Options: kick all tracked members from those groups, or keep them. Currently `deleteCreatorData` removes the creator record and managed-group state but does not touch the Telegram groups themselves, so members remain there as orphans. The same choice should apply to the "reset both" flow.
- **Unregister group**: let a creator unlink one managed Telegram group without deleting the entire creator record (e.g. `/unregistergroup` or an inline button). It should ask what to do with the current tracked members of that group: kick them, or leave them in place. Right now the only way to detach a managed group is to delete the creator entirely or remove the bot from the chat.
- **Bot removal auto-unregister**: when Telegram sends a `my_chat_member` update showing the bot was removed or kicked from a managed group, automatically unregister that group and notify the owner. For now the code only logs this as the next step.
- **Pre-populated group handling**: when a creator runs `/registergroup` on a group that already has members, decide what to do: kick everyone who isn't a verified subscriber, invite existing members to verify via `/start`, or ignore them. Currently the bot ignores pre-existing members entirely.
- **Untracked member policy**: the bot now records untracked users seen via `chat_member` updates and group messages. They are observational only: resets and automatic removal paths act only on tracked memberships. The next step is a creator-configurable response policy: ignore, DM to verify, start a grace period, or kick.
- **Forum / multi-topic group support**: Telegram supergroups can have topics enabled. Verify that invite links, kicks, and join request approvals work correctly in topic-enabled groups.

### GDPR compliance

- **Data export**: provide a `/export` command (or API endpoint) that lets a user download all personal data the bot holds about them (Telegram ID, Twitch links, group memberships, subscription history, timestamps) in a machine-readable format (JSON).
- **Right to erasure**: the existing reset flow deletes user data, but it should also confirm deletion of all associated records (event logs, untracked membership observations, cached invite links) and return a confirmation receipt.
- **Consent flow**: before storing any personal data, present a clear consent prompt explaining what data is collected, why, and how long it is retained. Store the consent timestamp.
- **Data retention policy**: define and enforce retention limits for event logs, untracked membership records, and OAuth tokens. Automatically purge data beyond the retention window.
- **Privacy policy link**: surface a link to the privacy policy in the `/start` flow and `/help` output.

### Data model hygiene

- **Tracked-group reverse-index hygiene**: keep `imsub:user:groups:tracked:*` consistent with canonical `imsub:group:tracked:*` sets; run periodic repair to fix stale or missing links.

### UX polish

- **`/help` command**: add a `/help` command that explains what the bot does, lists available commands, and provides a way to reach support (e.g. a link to a support channel, a contact username, or an inline form to submit issues). Could later evolve into an FAQ or guided troubleshooting flow.
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
