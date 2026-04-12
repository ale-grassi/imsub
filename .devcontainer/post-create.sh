#!/usr/bin/env bash

set -euo pipefail

configure_git() {
  mkdir -p /home/vscode/.config/git

  if [ -f /mnt/host-git-config/config ]; then
    git config --global --replace-all include.path /mnt/host-git-config/config
  fi

  if [ -x /usr/local/bin/op-ssh-sign ] && [ -f /mnt/host-git-config/allowed_signers ]; then
    git config --global gpg.format ssh
    git config --global gpg.ssh.program /usr/local/bin/op-ssh-sign
    git config --global gpg.ssh.allowedsignersfile /mnt/host-git-config/allowed_signers

    signing_key_file=/home/vscode/.config/git/signing_key.pub
    allowed_signer_key="$(awk 'NF >= 3 && $2 ~ /^ssh-/ { print $2 " " $3; exit }' /mnt/host-git-config/allowed_signers)"
    if [ -n "${allowed_signer_key:-}" ]; then
      printf '%s\n' "$allowed_signer_key" > "$signing_key_file"
      git config --global user.signingkey "$signing_key_file"
    else
      signing_key="$(git config --global --get user.signingkey || git config --get user.signingkey || true)"
      if [ -n "${signing_key:-}" ] && [ ! -f "$signing_key" ]; then
        candidate="/home/vscode/.ssh/$(basename "$signing_key")"
        if [ -f "$candidate" ]; then
          git config --global user.signingkey "$candidate"
        fi
      fi
    fi

    git config --global commit.gpgsign true
  fi
}

install_repo_hooks() {
  if [ -d /workspace/.git ]; then
    (cd /workspace && pre-commit install)
  fi
}

main() {
  mkdir -p "$HOME/.cache/go-build" "$HOME/.cache/gomod" "$HOME/.cache/golangci-lint"
  configure_git
  install_repo_hooks
  go mod download
}

main "$@"
