ARG BASE_IMAGE=mcr.microsoft.com/devcontainers/base:ubuntu-22.04
ARG GO_IMAGE=golang:1.26-bookworm
ARG NODE_IMAGE=node:24-bookworm-slim

FROM ${GO_IMAGE} AS go-toolchain

FROM ${NODE_IMAGE} AS node-toolchain

FROM ${BASE_IMAGE} AS devcontainer

ARG TARGETARCH
ARG GOLANGCI_LINT_VERSION=v2.10.0
ARG FLYCTL_VERSION=
ARG TWITCH_CLI_VERSION=v1.1.24

ENV DEBIAN_FRONTEND=noninteractive
ENV PATH=/usr/local/go/bin:/usr/local/bin:${PATH}
ENV GOCACHE=/tmp/gocache
ENV GOMODCACHE=/tmp/gomodcache
ENV GOBIN=/usr/local/bin

COPY --from=go-toolchain /usr/local/go /usr/local/go
COPY --from=node-toolchain /usr/local/bin /usr/local/bin
COPY --from=node-toolchain /usr/local/lib /usr/local/lib
COPY --from=node-toolchain /usr/local/include /usr/local/include
COPY --from=node-toolchain /usr/local/share /usr/local/share

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      python3-pip \
      python3-venv \
      redis-tools \
      xdg-utils \
    && rm -rf /var/lib/apt/lists/*

RUN python3 -m pip install --no-cache-dir pre-commit

RUN --mount=type=cache,target=/tmp/gocache \
    --mount=type=cache,target=/tmp/gomodcache \
    go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} \
    && go install golang.org/x/vuln/cmd/govulncheck@latest \
    && go install github.com/zricethezav/gitleaks/v8@latest \
    && go install github.com/rhysd/actionlint/cmd/actionlint@latest

RUN npm install -g ngrok @openai/codex @anthropic-ai/claude-code @google/gemini-cli

RUN arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
      amd64) twitch_asset="twitch-cli_${TWITCH_CLI_VERSION#v}_Linux_x86_64.tar.gz" ;; \
      arm64) twitch_asset="twitch-cli_${TWITCH_CLI_VERSION#v}_Linux_arm64.tar.gz" ;; \
      *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac \
    && tmp_dir="$(mktemp -d)" \
    && curl -fsSL "https://github.com/twitchdev/twitch-cli/releases/download/${TWITCH_CLI_VERSION}/${twitch_asset}" -o "${tmp_dir}/twitch-cli.tar.gz" \
    && tar -xzf "${tmp_dir}/twitch-cli.tar.gz" -C "${tmp_dir}" \
    && install -m 0755 "$(find "${tmp_dir}" -type f -name twitch -print -quit)" /usr/local/bin/twitch \
    && rm -rf "${tmp_dir}"

RUN tmp_dir="$(mktemp -d)" \
    && export FLYCTL_INSTALL="${tmp_dir}" \
    && curl -fsSL https://fly.io/install.sh | sh -s -- --non-interactive \
    && install -m 0755 "${tmp_dir}/bin/flyctl" /usr/local/bin/flyctl \
    && ln -sf /usr/local/bin/flyctl /usr/local/bin/fly \
    && rm -rf "${tmp_dir}"

USER vscode
