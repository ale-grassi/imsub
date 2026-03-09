#!/usr/bin/env bash

set -euo pipefail

install_flyctl() {
  if command -v flyctl >/dev/null 2>&1; then
    return
  fi

  curl -fsSL https://fly.io/install.sh | sh -s -- --non-interactive
  sudo ln -sf /home/vscode/.fly/bin/flyctl /usr/local/bin/fly
  sudo ln -sf /home/vscode/.fly/bin/flyctl /usr/local/bin/flyctl
}

install_apt_packages() {
  if command -v xdg-open >/dev/null 2>&1 && command -v redis-cli >/dev/null 2>&1; then
    return
  fi

  export DEBIAN_FRONTEND=noninteractive
  sudo apt-get update
  sudo apt-get install -y --no-install-recommends redis-tools xdg-utils
}

install_go_tools() {
  local gopath_bin
  gopath_bin="$(go env GOPATH)/bin"

  if [ ! -x "${gopath_bin}/golangci-lint" ]; then
    GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache \
      go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.10.0
  fi

  if [ ! -x "${gopath_bin}/govulncheck" ]; then
    GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache \
      go install golang.org/x/vuln/cmd/govulncheck@latest
  fi

  if [ ! -x "${gopath_bin}/gitleaks" ]; then
    GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache \
      go install github.com/zricethezav/gitleaks/v8@latest
  fi
}

install_ai_clis() {
  if ! command -v npm >/dev/null 2>&1; then
    echo "npm not found; skipping Codex, Claude Code, and Gemini CLI installation" >&2
    return
  fi

  local npm_global_bin
  npm_global_bin="$(npm config get prefix)/bin"

  if [ ! -x "${npm_global_bin}/codex" ]; then
    npm install -g @openai/codex
  fi

  if [ ! -x "${npm_global_bin}/claude" ]; then
    npm install -g @anthropic-ai/claude-code
  fi

  if [ ! -x "${npm_global_bin}/gemini" ]; then
    npm install -g @google/gemini-cli
  fi
}

configure_git() {
  mkdir -p /home/vscode/.config/git

  if [ -f /mnt/host-git-config/config ]; then
    git config --global --replace-all include.path /mnt/host-git-config/config
  fi

  if [ -x /usr/local/bin/op-ssh-sign ] && [ -f /mnt/host-git-config/allowed_signers ]; then
    git config --global gpg.format ssh
    git config --global gpg.ssh.program /usr/local/bin/op-ssh-sign
    git config --global gpg.ssh.allowedsignersfile /mnt/host-git-config/allowed_signers
    git config --global commit.gpgsign true
  fi
}

main() {
  install_flyctl
  install_apt_packages
  install_go_tools
  install_ai_clis
  configure_git
  go mod download
}

main "$@"
