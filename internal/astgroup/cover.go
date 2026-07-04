package astgroup

import "strconv"

// blockKinds are the node kinds that constitute a "logical block" — a scope a
// finding can be grouped by. Leaf/simple statements (assign, return, expr, bare
// stmt) are intentionally excluded: mapping a finding to its enclosing block
// (function body, control-flow block, class) rather than to the exact statement
// is what lets two findings that drifted by whitespace — or that a skeptic
// flagged a few lines apart inside the same block — group together, while keeping
// findings in genuinely different blocks apart.
//
// Python compound-statement clauses (else / elif / except / finally) are emitted
// by the pyparser plugin as SIBLING blocks of their if/try rather than children,
// because they share the opener's indentation. Consequence for CoveringBlock: an
// if node's span excludes its else body, so a finding in the if arm and one in
// the else arm of the same statement never share a covering block, and a chained
// if/elif (elif is remapped to kind "if") reads as several independent sibling
// blocks. This is intentional — clause-level granularity, not whole-compound
// grouping — and the kinds listed below reflect it.
var blockKinds = map[string]bool{
	"file": true, "module": true, "func": true, "funclit": true,
	"class": true, "type": true, "gendecl": true, "block": true,
	"if": true, "for": true, "while": true, "switch": true,
	"with": true, "try": true, "except": true, "finally": true, "else": true,
}

func isBlock(n Node) bool { return blockKinds[n.Kind] }

func segment(n Node, blockIdx int) string {
	return n.Kind + ":" + n.Name + ":" + strconv.Itoa(blockIdx)
}

// CoveringBlock returns the deepest block-level node whose inclusive line span
// contains line, together with a structural address that uniquely locates that
// block within the tree. The address is a "/"-joined chain of
// kind:name:sibling-block-index segments from the root — fully structural, so it
// is invariant to line-number drift (blank lines, reformatting) yet distinguishes
// sibling blocks and identically-shaped blocks in different scopes. ok is false
// when line falls outside the root's span.
func CoveringBlock(root Node, line int) (block Node, addr string, ok bool) {
	if line < root.StartLine || line > root.EndLine {
		return Node{}, "", false
	}
	chain, subAddr := coveringChain(root, line)
	addr = segment(root, 0)
	block = root
	if len(chain) > 0 {
		addr += "/" + subAddr
		block = chain[len(chain)-1]
	}
	return block, addr, true
}

// coveringChain returns the chain of block nodes BELOW n down to the deepest
// covering block, and the address of that chain relative to n. It descends
// through non-block nodes (e.g. an expression statement wrapping a function
// literal) to reach nested blocks. The returned chain excludes n itself.
//
// Invariant: a node's children are assumed to have non-overlapping line ranges.
// The function returns the first covering child it finds; overlapping children
// would break the uniqueness guarantee of the returned address.
func coveringChain(n Node, line int) (chain []Node, addr string) {
	blockIdx := 0
	nonBlockIdx := 0
	for i := range n.Children {
		ch := n.Children[i]
		block := isBlock(ch)
		if line < ch.StartLine || line > ch.EndLine {
			if block {
				blockIdx++
			} else {
				nonBlockIdx++
			}
			continue
		}
		subChain, subAddr := coveringChain(ch, line)
		if block {
			seg := segment(ch, blockIdx)
			if subAddr != "" {
				seg += "/" + subAddr
			}
			return append([]Node{ch}, subChain...), seg
		}
		// ch covers but is not a block. If it has no block descendant the cover is
		// n itself (the enclosing block), so collapse to it — this keeps leaf
		// statements in one block grouping together.
		if len(subChain) == 0 {
			return nil, ""
		}
		// ch wraps a block descendant: carry ch's sibling position into the
		// address so descendants under distinct non-block siblings (e.g. two
		// exprstmts each wrapping an identically-shaped funclit) get distinct
		// addresses instead of both recomputing from index 0.
		return subChain, segment(ch, nonBlockIdx) + "/" + subAddr
	}
	return nil, ""
}

// SmallestCovering returns the deepest block-level node covering line, or nil if
// line is outside the root. It is CoveringBlock without the address.
func SmallestCovering(root Node, line int) *Node {
	block, _, ok := CoveringBlock(root, line)
	if !ok {
		return nil
	}
	return &block
}

// EnclosingSymbolName returns the name of the nearest enclosing NAMED block
// covering line: it walks the covering chain from the deepest covering block up
// toward the root and returns the first node whose Name is non-empty — a
// func/method/class/type. Unnamed control-flow blocks (if/for/while/switch/…) are
// skipped, so a finding inside an `if` inside `classifyHeader` resolves to
// "classifyHeader", not the anonymous `if`. ok is false when line falls outside
// root or no covering node is named; the caller then omits the symbol prefix
// (the AC2 graceful-degradation path).
//
// Unlike CoveringBlock's key, the returned name is a human-facing display anchor
// (a stable RELOCATE_KEY for resolve-td), not a grouping key: duplicate names
// within a file are harmless here.
func EnclosingSymbolName(root Node, line int) (string, bool) {
	if line < root.StartLine || line > root.EndLine {
		return "", false
	}
	// coveringChain returns the block nodes BELOW root down to the deepest covering
	// block, ordered shallow→deep; walk it deep→shallow so the tightest named scope
	// around the finding wins.
	chain, _ := coveringChain(root, line)
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].Name != "" {
			return chain[i].Name, true
		}
	}
	// The chain excludes root itself; a named root (rare — file/module nodes are
	// unnamed) is still a valid outermost anchor.
	if root.Name != "" {
		return root.Name, true
	}
	return "", false
}
