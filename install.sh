#!/usr/bin/env bash
# atcr install.sh — thin wrapper around `go install …@latest` with a Go-toolchain
# preflight and a non-fatal PATH containment check.
#
# Usage:    ./install.sh
# Requires: a Go 1.25+ toolchain on PATH.
set -euo pipefail

# 1. Go-toolchain preflight — fail fast before attempting any install.
if ! command -v go >/dev/null 2>&1; then
  echo "error: go toolchain not found. Install Go 1.25+ from https://go.dev/dl/ and re-run this script." >&2
  exit 1
fi

# 2. Install the latest published atcr binary (real, unsuppressed exit code + stderr).
go install github.com/samestrin/atcr/cmd/atcr@latest

# 3. Success guidance.
echo "atcr installed. Next: run 'atcr doctor' to verify your setup, or 'atcr version'."

# 4. Non-fatal PATH containment check — exact colon-delimited segment match, never substring.
gobin="$(go env GOPATH)/bin"
on_path=0
# ${PATH:-} guards `set -u`: a stripped environment with PATH unset must not abort
# the colon-split. Empty segments (e.g. a trailing colon) never equal a non-empty
# gobin, so they are harmless to the containment result.
IFS=':' read -ra segments <<<"${PATH:-}"
for seg in "${segments[@]}"; do
  if [ "$seg" = "$gobin" ]; then on_path=1; break; fi
done
if [ "$on_path" -eq 0 ]; then
  echo "warning: $gobin is not on your PATH. Add it with: export PATH=\"$gobin:\$PATH\"" >&2
fi
