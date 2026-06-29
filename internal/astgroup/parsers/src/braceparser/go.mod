// Isolated module: built only for GOOS=wasip1 via build.sh, once per target
// language (-tags ts|php|rust|bash). A nested go.mod keeps `go build ./...` /
// `go test ./...` in the parent reconcile module from compiling this wasm-only
// command (it uses //go:wasmexport + package main). The language-agnostic
// scanner (parse_core.go / configs.go) carries no build tag, so it is unit-
// tested on the host via `go test` in this directory.
module github.com/samestrin/atcr/internal/astgroup/parsers/src/braceparser

go 1.26
