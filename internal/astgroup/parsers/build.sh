#!/usr/bin/env bash
# Regenerate the vendored WebAssembly parser plugins from their Go sources under
# src/. The resulting .wasm files are committed (vendored) and embedded by the
# astgroup host via go:embed, so this script is only needed when a parser source
# changes — it is NOT part of the normal `go build` / CI path (no Wasm toolchain
# beyond the standard Go compiler is required).
#
# Usage: internal/astgroup/parsers/build.sh
set -euo pipefail

# Pin the toolchain and assert a minimum Go version so the regenerated .wasm is
# reproducible and an older `go` on PATH fails fast instead of emitting a
# silently-incompatible binary. The wasip1 //go:wasmexport ABI needs Go >= 1.24;
# GOTOOLCHAIN=local also forbids an implicit toolchain download, so a committed
# go.mod `go` directive cannot silently swap compilers mid-build. Source/binary
# drift is caught separately by TestEmbeddedParsersMatchManifest (go test ./...).
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
min_go="1.24"
have_go="$(go env GOVERSION 2>/dev/null)"; have_go="${have_go#go}"
if [ -z "${have_go}" ] || \
   [ "$(printf '%s\n%s\n' "${min_go}" "${have_go}" | sort -V | head -n1)" != "${min_go}" ]; then
  echo "error: build.sh requires Go >= ${min_go} for the wasip1 wasmexport ABI; found '${have_go:-none}'" >&2
  exit 1
fi

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

build() {
  local name="$1" srcdir="$2"
  echo "building ${name}.wasm from ${srcdir}"
  ( cd "${here}/src/${srcdir}" && \
    GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -trimpath \
      -o "${here}/${name}.wasm" . )
  echo "  -> $(wc -c < "${here}/${name}.wasm") bytes"
}

build go goparser
build python pyparser

# Refresh the checksum manifest so the committed binaries stay verifiable from a
# committed hash. TestEmbeddedParsersMatchManifest (go test ./...) fails in CI if
# the .wasm files and SHA256SUMS drift, catching a tampered or stale binary
# without needing a Wasm toolchain in the pipeline. Commit SHA256SUMS with the
# regenerated .wasm files.
( cd "${here}" && sha256sum go.wasm python.wasm > SHA256SUMS )
echo "wrote SHA256SUMS"
echo "done"
