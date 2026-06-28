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
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"unsafe"
)

// node mirrors internal/astgroup.Node; the JSON tags are the wire contract.
type node struct {
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Children  []node `json:"children,omitempty"`
}

// pins keeps alloc'd buffers reachable so the Go GC cannot reclaim memory the
// host still references. The wasm linear memory IS this program's heap, so a
// pinned slice's first-element pointer is a stable guest offset. This assumes
// the wasm-targeting Go GC remains non-moving (true for Go 1.21+ wasip1/wasm);
// review this assumption before upgrading the toolchain.
var pins = map[int32][]byte{}

//go:wasmexport alloc
func alloc(n int32) int32 {
	if n <= 0 {
		n = 1
	}
	b := make([]byte, n)
	p := int32(uintptr(unsafe.Pointer(&b[0])))
	pins[p] = b
	return p
}

//go:wasmexport free
func free(p int32) { delete(pins, p) }

//go:wasmexport parse
func parse(ptr int32, n int32) int64 {
	buf, ok := pins[ptr]
	if !ok || int(n) > len(buf) {
		return emit(node{Kind: "error", Name: "bad pointer"})
	}
	src := buf[:n]

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.SkipObjectResolution)
	if err != nil {
		// Even on syntax error, emit a minimal file node so the host can fall
		// back to line-proximity grouping rather than crashing.
		return emit(node{Kind: "error", Name: err.Error()})
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
	return emit(root)
}

// structural reports whether an ast.Node is a node kind we keep in the tree.
// Declarations, statements, and function literals carry structure; expressions,
// identifiers, and literals are summarized into their parent's Kind/Name.
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
	case *ast.TypeSpec:
		return "type"
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
	case *ast.TypeSpec:
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

func emit(n node) int64 {
	b, err := json.Marshal(n)
	if err != nil {
		b = []byte(`{"kind":"error","name":"marshal"}`)
	}
	p := alloc(int32(len(b)))
	copy(pins[p], b)
	return (int64(p) << 32) | int64(len(b))
}

func main() {}
