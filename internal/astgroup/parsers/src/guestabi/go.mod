// Isolated module: built only for GOOS=wasip1 via build.sh. A nested go.mod
// keeps `go build ./...` / `go test ./...` in the parent module from compiling
// this wasm-only guest ABI package (it uses unsafe pointer-packing valid only
// under the non-moving wasip1/wasm GC). Imported by the sibling goparser,
// pyparser, and braceparser command modules via `replace => ../guestabi`.
module github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi

go 1.26
