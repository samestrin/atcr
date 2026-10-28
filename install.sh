#!/usr/bin/env bash
# atcr install.sh — thin wrapper around `go install` with a Go-toolchain preflight
# and a non-fatal PATH containment check. Run from an atcr clone, it installs that
# checkout's code; anywhere else it installs the latest published release.
#
# Usage:    ./install.sh
# Requires: a Go 1.25+ toolchain on PATH.
set -euo pipefail

# 1. Go-toolchain preflight — fail fast before attempting any install.
if ! command -v go >/dev/null 2>&1; then
  echo "error: go toolchain not found. Install Go 1.25+ from https://go.dev/dl/ and re-run this script." >&2
  exit 1
fi

# 1b. Enforce the documented Go 1.25+ minimum. The check above proves `go` exists but
#     not that it is new enough; without this, an old toolchain fails later with a
#     cryptic `go install` error instead of a clear, actionable message. Only a
#     cleanly-parsed version below 1.25 is rejected — an unparseable or devel version
#     (e.g. "go1.26-devel_...") is allowed through rather than risk blocking a valid
#     toolchain.
goversion="$(go env GOVERSION 2>/dev/null)"
if [[ "$goversion" =~ ^go([0-9]+)\.([0-9]+) ]]; then
  gmaj="${BASH_REMATCH[1]}"
  gmin="${BASH_REMATCH[2]}"
  if [ "$gmaj" -lt 1 ] || { [ "$gmaj" -eq 1 ] && [ "$gmin" -lt 25 ]; }; then
    echo "error: Go ${goversion#go} is too old; atcr requires Go 1.25+. Upgrade from https://go.dev/dl/ and re-run this script." >&2
    exit 1
  fi
fi

# 2. Install atcr (real, unsuppressed exit code + stderr). When this script sits in
#    an atcr checkout, build that checkout: a user who cloned the repo expects the
#    clone's code, and `@latest` resolves to the last tag, which can be older.
#    Otherwise (a copy of the script on its own, or piped into bash, where there is
#    no script file and so no checkout) install the latest release.
script_dir=""
if [ -n "${BASH_SOURCE[0]:-}" ]; then
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
if [ -n "$script_dir" ] && [ -d "$script_dir/cmd/atcr" ] && grep -qx 'module github.com/samestrin/atcr' "$script_dir/go.mod" 2>/dev/null; then
  echo "Installing atcr from the checkout at $script_dir"
  (cd "$script_dir" && go install ./cmd/atcr)
else
  go install github.com/samestrin/atcr/cmd/atcr@latest
fi

# 3. Non-fatal PATH containment check — exact colon-delimited segment match, never
#    substring. Computed BEFORE the success guidance so "run atcr doctor" is only
#    advised once PATH actually contains the install dir; otherwise the user is first
#    shown how to fix PATH rather than told to run a command that is not yet callable.
# Resolve the real install target: GOBIN wins when set (that is where `go install`
# writes the binary); otherwise fall back to GOPATH/bin.
gobin="$(go env GOBIN)"
if [ -z "$gobin" ]; then
  # GOPATH may be a colon-delimited list; go install writes to the FIRST element's
  # bin, so take that element rather than appending /bin to the whole list.
  gopath="$(go env GOPATH)"
  gobin="${gopath%%:*}/bin"
fi
on_path=0
# ${PATH:-} guards `set -u`: a stripped environment with PATH unset must not abort
# the colon-split. Empty segments (e.g. a trailing colon) never equal a non-empty
# gobin, so they are harmless to the containment result.
# Normalize a single trailing slash so /foo/bin and /foo/bin/ compare equal. Matching
# stays an EXACT per-segment comparison (never substring/prefix) so a partial-collision
# segment like /foo/bin-partial still does not count as on-PATH.
gobin="${gobin%/}"
IFS=':' read -ra segments <<<"${PATH:-}"
for seg in "${segments[@]}"; do
  if [ "${seg%/}" = "$gobin" ]; then on_path=1; break; fi
done

# 4. Success guidance — tailored to PATH status.
echo "atcr installed."
if [ "$on_path" -eq 0 ]; then
  echo "warning: $gobin is not on your PATH. Add it with: export PATH=\"$gobin:\$PATH\"" >&2
  echo "Once it is on your PATH, run 'atcr doctor' to verify your setup, or 'atcr version'." >&2
else
  echo "Next: run 'atcr doctor' to verify your setup, or 'atcr version'."
fi
