//go:build wasip1

// Command goparser is the Go-language AST parser plugin, compiled to a
// WebAssembly reactor (GOOS=wasip1 GOARCH=wasm, -buildmode=c-shared) and loaded
// by the internal/astgroup wazero host. It reads Go source from guest linear
// memory and emits a normalized structural node tree as JSON.
//
// The JSON contract (kind / name / start_line / end_line / children) is shared
// with every other language plugin so the host stays language-agnostic. Line
// numbers are 1-based and inclusive. Identifier names are included so that
// sibling declarations with identical shape (e.g. two empty functions) hash
// differently in the host's Merkle pass, while whitespace / blank-line drift —
// which changes line numbers but not node kinds, names, or nesting — produces an
// identical structural hash.
//
// Memory protocol (matches internal/astgroup wazero host):
//   - alloc(n) returns a guest pointer to n writable bytes, pinned against GC.
//   - parse(ptr, n) parses src=memory[ptr:ptr+n] and returns (resPtr<<32|resLen).
//   - free(ptr) releases a previously alloc'd pointer.
//
// Regenerate the vendored .wasm via internal/astgroup/parsers/build.sh.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi"
)

// node mirrors internal/astgroup.Node; the JSON tags are the wire contract.
type node struct {
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Children  []node `json:"children,omitempty"`
}

// alloc/free/parse are the wasip1 reactor ABI entrypoints the astgroup host
// calls. Go requires //go:wasmexport functions in each command's own package
// main, so these thin wrappers just delegate to the shared guestabi bodies (see
// the guestabi package doc for the pin map and its GC assumptions).

//go:wasmexport alloc
func alloc(n int32) int32 { return guestabi.Alloc(n) }

//go:wasmexport free
func free(p int32) { guestabi.Free(p) }

//go:wasmexport parse
func parse(ptr int32, n int32) int64 {
	buf, ok := guestabi.Lookup(ptr)
	if !ok || int(n) < 0 || int(n) > len(buf) {
		return guestabi.Emit(node{Kind: "error", Name: "bad pointer"})
	}
	src := buf[:n]

	// Empty input has no declarations to parse. Emit a bare file node (Kind=file,
	// zero start/end lines) so the host treats empty source as an empty tree
	// rather than a parse error — pinned by TestHost_ParseEmptySourceGo. The
	// other plugins' empty-source nodes are NOT byte-identical to this one
	// (pyparser emits Kind=module 1/1; braceparser emits Kind=file 1/1): the
	// shared contract is "empty source is an empty tree, not an error", not
	// cross-plugin JSON parity. Non-empty but unparseable source still falls
	// through to the error node below.
	if len(src) == 0 {
		return guestabi.Emit(node{Kind: "file"})
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.SkipObjectResolution)
	if err != nil {
		// Even on syntax error, emit a minimal file node so the host can fall
		// back to line-proximity grouping rather than crashing.
		return guestabi.Emit(node{Kind: "error", Name: err.Error()})
	}

	line := func(p token.Pos) int {
		if !p.IsValid() {
			return 0
		}
		return fset.Position(p).Line
	}

	var build func(n ast.Node) node
	build = func(an ast.Node) node {
		nd := node{Kind: nodeKind(an), StartLine: line(an.Pos()), EndLine: line(an.End())}
		nd.Name = nodeName(an)
		// Collect direct structural children (skip identifiers/literals — they
		// are folded into Name/Kind, not separate structural nodes).
		ast.Inspect(an, func(child ast.Node) bool {
			if child == nil || child == an {
				return true
			}
			if structural(child) {
				nd.Children = append(nd.Children, build(child))
				return false // don't descend; build recurses itself
			}
			return true
		})
		return nd
	}

	root := build(f)
	return guestabi.Emit(root)
}

// structural reports whether an ast.Node is a node kind we keep in the tree.
// Declarations, statements, and function literals carry structure; expressions,
// identifiers, and literals are summarized into their parent's Kind/Name.
//
// ast.TypeSpec is an ast.Spec, not an ast.Decl, so it is intentionally excluded:
// types are grouped at GenDecl granularity (a `type Foo struct{...}` surfaces as
// its wrapping gendecl block), mirroring how the Python plugin groups class/func
// but not individual fields. Emitting per-TypeSpec nodes would change the host
// Merkle hash with no grouping benefit, so nodeKind/nodeName carry no type case.
func structural(n ast.Node) bool {
	switch n.(type) {
	case ast.Decl, ast.Stmt, *ast.FuncLit:
		return true
	default:
		return false
	}
}

func nodeKind(n ast.Node) string {
	switch n.(type) {
	case *ast.File:
		return "file"
	case *ast.FuncDecl:
		return "func"
	case *ast.GenDecl:
		return "gendecl"
	default:
		// Strip the leading *ast. and trailing Stmt/Decl noise for stable kinds.
		t := typeName(n)
		return t
	}
}

func nodeName(n ast.Node) string {
	switch d := n.(type) {
	case *ast.FuncDecl:
		if d.Name != nil {
			return d.Name.Name
		}
	}
	return ""
}

func typeName(n ast.Node) string {
	switch n.(type) {
	case *ast.BlockStmt:
		return "block"
	case *ast.IfStmt:
		return "if"
	case *ast.ForStmt, *ast.RangeStmt:
		return "for"
	case *ast.SwitchStmt, *ast.TypeSwitchStmt:
		return "switch"
	case *ast.ReturnStmt:
		return "return"
	case *ast.AssignStmt:
		return "assign"
	case *ast.ExprStmt:
		return "expr"
	case *ast.FuncLit:
		return "funclit"
	default:
		return "stmt"
	}
}

func main() {}
