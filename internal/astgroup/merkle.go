package astgroup

import (
	"crypto/sha256"
	"encoding/hex"
)

// maxNodeDepth bounds the recursion depth of the structural walks over a parsed
// tree. It is a contract-level cap independent of the JSON decoder's nesting
// limit: subtrees deeper than this are truncated by merkleSum so a pathologically
// deep (e.g. hostile override-plugin) tree cannot drive unbounded host stack
// growth. Real code nests only dozens of levels, so this never triggers in
// practice; the host also rejects over-deep trees at decode time.
const maxNodeDepth = 4096

// MerkleHash computes a structural hash of a node: the hash folds in the node's
// Kind and Name and, recursively, the hashes of its children, but NOT line
// numbers. Two nodes therefore hash identically when they have the same kind,
// name, and child structure even if their reported line numbers differ — exactly
// the whitespace / blank-line / minor-drift invariance AC3 requires — while
// differing in kind, name, or shape yields a different hash so distinct sibling
// blocks are not over-merged.
//
// The encoding is unambiguous: each node serializes as
// kind \x00 name '(' childSum ',' childSum ',' ... ')', so no kind/name/child
// arrangement can be confused with another.
func MerkleHash(n Node) string {
	sum := merkleSum(n, 0)
	return hex.EncodeToString(sum[:])
}

// merkleSum is the recursive core of MerkleHash. It folds children's raw 32-byte
// sums (not their 64-char hex strings) into the parent, so internal nodes neither
// allocate a hex string per level nor feed twice the necessary bytes into sha256.
// Only the public MerkleHash hex-encodes, and only the root. Recursion is capped
// at maxNodeDepth: at the cap a node is hashed without descending into its
// children, bounding stack depth.
func merkleSum(n Node, depth int) [32]byte {
	h := sha256.New()
	h.Write([]byte(n.Kind))
	h.Write([]byte{0})
	h.Write([]byte(n.Name))
	h.Write([]byte{'('})
	if depth < maxNodeDepth {
		for _, c := range n.Children {
			cs := merkleSum(c, depth+1)
			h.Write(cs[:])
			h.Write([]byte{','})
		}
	}
	h.Write([]byte{')'})
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
