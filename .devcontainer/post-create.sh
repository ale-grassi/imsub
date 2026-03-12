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
    git config --global commit.gpgsign true
  fi
}

install_repo_hooks() {
  if [ -d /workspace/.git ]; then
    (cd /workspace && pre-commit install)
  fi
}

main() {
  configure_git
  install_repo_hooks
  go mod download
}

main "$@"
