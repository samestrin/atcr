// Isolated module: built only for GOOS=wasip1 via build.sh. A nested go.mod
// keeps `go build ./...` / `go test ./...` in the parent reconcile module from
// compiling this wasm-only command (it uses //go:wasmexport + package main).
module github.com/samestrin/atcr/internal/astgroup/parsers/src/goparser

go 1.26
