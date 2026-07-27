package astgroup

// SkeletonEntry is one top-level declaration header extracted from a parsed
// source tree. Header is the declaration's signature line with the body elided —
// enough for a reviewer to see the file's resolved shape without paying for its
// full text.
type SkeletonEntry struct {
	Kind      string
	Name      string
	StartLine int
	Header    string
}

// FileSkeleton returns one entry per top-level func/gendecl declaration in root,
// in source order, with each header sliced from src.
func FileSkeleton(root Node, src []byte) []SkeletonEntry {
	return nil
}

// Cyclomatic returns the McCabe cyclomatic complexity of the tree rooted at
// root: the number of branch-kind nodes plus one.
func Cyclomatic(root Node) int {
	return 0
}
