package astgroup

// SymbolDecl is one named block declared in a parsed source tree: the
// identifier the parser plugin attached to the node, and the 1-based line the
// node starts on.
//
// It is the inverse direction of EnclosingSymbolName (Epic 18.1), which
// resolves a known line to its enclosing symbol. Epic 35.16.6.5's Tier 4 path
// resolution needs symbol -> line instead, so a finding that cites a file which
// does not exist can still be located by the construct it describes.
type SymbolDecl struct {
	Name string
	Line int
}

// NamedSymbols returns every named block in the tree rooted at root, in
// document order (pre-order, parents before children). Unnamed structural nodes
// — file/module wrappers, bare blocks, anonymous functions — contribute
// nothing, and a tree with no named block returns nil.
//
// Duplicate names are NOT collapsed: two same-named methods on different types
// are two distinct declarations, and the caller (the Tier 4 symbol index) needs
// both sites to judge whether a match is unambiguous. Pre-order is a property of
// the walk, not of map iteration, so the result is byte-stable across runs for a
// given tree.
func NamedSymbols(root Node) []SymbolDecl {
	var out []SymbolDecl
	appendNamedSymbols(root, &out)
	return out
}

// appendNamedSymbols is the pre-order walk backing NamedSymbols. It is written
// against a *[]SymbolDecl rather than returning slices so a deep tree does not
// allocate one slice per level.
func appendNamedSymbols(n Node, out *[]SymbolDecl) {
	if n.Name != "" {
		*out = append(*out, SymbolDecl{Name: n.Name, Line: n.StartLine})
	}
	for _, ch := range n.Children {
		appendNamedSymbols(ch, out)
	}
}
