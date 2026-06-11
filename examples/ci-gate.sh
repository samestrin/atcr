#!/usr/bin/env bash
# atcr CI gate — fail the build when findings at/above a severity survive review.
#
# One-shot mode: `atcr review --fail-on` resolves the range, fans out, reconciles,
# and gates the exit code in a single command. Exit codes: 0 pass, 1 gate failure
# (findings at/above threshold survive), 2 usage/config error.
#
# Usage:   examples/ci-gate.sh [base-ref]
# Env:     ATCR_FAIL_ON  severity threshold (default: high)
set -euo pipefail

FAIL_ON="${ATCR_FAIL_ON:-high}"
BASE_REF="${1:-}"

if [ -n "$BASE_REF" ]; then
  exec atcr review --base "$BASE_REF" --fail-on "$FAIL_ON"
fi

exec atcr review --fail-on "$FAIL_ON"
