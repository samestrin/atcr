package astgroup

import (
	"crypto/sha256"
	"encoding/hex"
)

// MerkleHash computes a structural hash of a node: the hash folds in the node's
// Kind and Name and, recursively, the hashes of its children, but NOT line
// numbers. Two nodes therefore hash identically when they have the same kind,
// name, and child structure even if their reported line numbers differ — exactly
// the whitespace / blank-line / minor-drift invariance AC3 requires — while
// differing in kind, name, or shape yields a different hash so distinct sibling
// blocks are not over-merged.
//
// The encoding is unambiguous: each node serializes as
// kind \x00 name '(' childHash ',' childHash ',' ... ')', so no kind/name/child
// arrangement can be confused with another.
func MerkleHash(n Node) string {
	h := sha256.New()
	h.Write([]byte(n.Kind))
	h.Write([]byte{0})
	h.Write([]byte(n.Name))
	h.Write([]byte{'('})
	for _, c := range n.Children {
		h.Write([]byte(MerkleHash(c)))
		h.Write([]byte{','})
	}
	h.Write([]byte{')'})
	return hex.EncodeToString(h.Sum(nil))
}
