#!/bin/bash

SCRIPT_DIR="$(dirname "$0")"
vault server \
  -dev \
  -dev-root-token-id=root \
  -config="${SCRIPT_DIR}/vault_config.hcl" \
  -log-level=debug>/tmp/vault.log 2>&1 &

echo $! > /tmp/vault.pid
