package astgroup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNamedSymbols_PreOrder verifies the inverse-direction walk (Epic
// 35.16.6.5 T2): every named block, in document order. Unnamed structural nodes
// contribute nothing, and nesting is followed.
func TestNamedSymbols_PreOrder(t *testing.T) {
	tree := Node{Kind: "file", StartLine: 1, EndLine: 40, Children: []Node{
		{Kind: "func", Name: "Alpha", StartLine: 3, EndLine: 9, Children: []Node{
			{Kind: "if", StartLine: 5, EndLine: 7}, // unnamed: skipped
			{Kind: "func", Name: "inner", StartLine: 8, EndLine: 8},
		}},
		{Kind: "block", StartLine: 11, EndLine: 12}, // unnamed: skipped
		{Kind: "type", Name: "Beta", StartLine: 15, EndLine: 30, Children: []Node{
			{Kind: "method", Name: "Close", StartLine: 20, EndLine: 25},
		}},
	}}

	assert.Equal(t, []string{"Alpha", "inner", "Beta", "Close"}, NamedSymbols(tree))
}

// TestNamedSymbols_NamedRoot covers the rare named root (a module node carrying
// its own name): it is a declaration like any other and is emitted first.
func TestNamedSymbols_NamedRoot(t *testing.T) {
	tree := Node{Kind: "module", Name: "mod", StartLine: 1, EndLine: 5, Children: []Node{
		{Kind: "func", Name: "f", StartLine: 2, EndLine: 3},
	}}
	assert.Equal(t, []string{"mod", "f"}, NamedSymbols(tree))
}

// TestNamedSymbols_DuplicatesPreserved pins that same-named declarations are
// NOT collapsed: the Tier 4 index needs both sites to decide a match is
// ambiguous rather than confidently suggesting the first one.
func TestNamedSymbols_DuplicatesPreserved(t *testing.T) {
	tree := Node{Kind: "file", StartLine: 1, EndLine: 20, Children: []Node{
		{Kind: "method", Name: "Close", StartLine: 4, EndLine: 6},
		{Kind: "method", Name: "Close", StartLine: 10, EndLine: 12},
	}}
	assert.Equal(t, []string{"Close", "Close"}, NamedSymbols(tree))
}

// TestNamedSymbols_Empty verifies a tree with no named block yields nil, so the
// index build adds nothing rather than an empty-string key.
func TestNamedSymbols_Empty(t *testing.T) {
	assert.Nil(t, NamedSymbols(Node{Kind: "file", StartLine: 1, EndLine: 3, Children: []Node{
		{Kind: "comment", StartLine: 2, EndLine: 2},
	}}))
}
