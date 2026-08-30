// Package mcp implements the atcr MCP stdio server: tool registration with
// typed schemas and thin handlers over the same engine packages the CLI uses.
//
// A test that swaps the readUnresolvedSidecar seam in handlers.go must NOT call
// t.Parallel, and no parallel test in this package may depend on it. It is a
// mutable package-level var, so a parallel body resuming while a swap is in
// effect would read the wrong reader. Mirrors the same caution in
// internal/reconcile/doc.go for its newTier4Index seam.
package mcp
