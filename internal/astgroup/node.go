// Package astgroup provides AST-isomorphism grouping for reconciler findings.
//
// It loads language parsers compiled to WebAssembly (one .wasm plugin per
// language, vendored under parsers/ and embedded via go:embed) and runs them on
// a pure-Go wazero runtime — no CGO, so the atcr binary still cross-compiles to
// every target out of the box. A finding's line number is mapped to its smallest
// covering AST node, and a structural Merkle hash of that node becomes a grouping
// key: findings that map to the same logical block group together even when their
// reported line numbers drift (whitespace, blank lines, minor model skew).
//
// This package lives in the root module, not the zero-dependency reconcile
// library: it implements reconcile's stdlib-only Grouper seam so wazero is
// integrated into the atcr reconciler without adding a dependency to the
// embeddable library.
package astgroup

// Node is one structural node in a parsed source tree. It is the language-
// agnostic contract every .wasm parser plugin emits as JSON; the host decodes it
// here. Line numbers are 1-based and inclusive. Name carries an identifier
// (function/class name) when the node kind has one, so that sibling blocks with
// identical shape still hash distinctly.
type Node struct {
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Children  []Node `json:"children,omitempty"`
}
