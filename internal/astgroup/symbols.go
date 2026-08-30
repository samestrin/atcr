package astgroup

// NamedSymbols returns the name of every named block in the tree rooted at root,
// in document order (pre-order, parents before children). Unnamed structural
// nodes — file/module wrappers, bare blocks, anonymous functions — contribute
// nothing, and a tree with no named block returns nil.
//
// It is the inverse direction of EnclosingSymbolName (Epic 18.1), which resolves
// a known line to its enclosing symbol. Epic 35.16.6.5's Tier 4 path resolution
// needs symbol -> file instead, so a finding that cites a file which does not
// exist can still be located by the construct it describes.
//
// Only names are returned. The declaring LINE is deliberately not carried: the
// Tier 4 contract this feeds is PathSuggestion, which is a path (the Epic 5.4
// shape it extends), so a line would be collected and never read.
//
// Duplicate names are NOT collapsed: two same-named methods on different types
// are two distinct declarations, and the caller (the Tier 4 symbol index) needs
// both sites to judge whether a match is unambiguous. Pre-order is a property of
// the walk, not of map iteration, so the result is byte-stable across runs for a
// given tree.
func NamedSymbols(root Node) []string {
	var out []string
	appendNamedSymbols(root, &out)
	return out
}

// appendNamedSymbols is the pre-order walk backing NamedSymbols. It is written
// against a *[]string rather than returning slices so a deep tree does not
// allocate one slice per level.
func appendNamedSymbols(n Node, out *[]string) {
	if n.Name != "" {
		*out = append(*out, n.Name)
	}
	for _, ch := range n.Children {
		appendNamedSymbols(ch, out)
	}
}
