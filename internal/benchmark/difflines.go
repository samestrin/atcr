package benchmark

import "errors"

// errNotImplemented is the deliberate wrong answer this stub returns so the RED
// test compiles and fails for a real reason.
var errNotImplemented = errors.New("diff line map not implemented")

// DiffLineMap classifies every line of a case's change.diff.
type DiffLineMap struct {
	added   map[string]map[int]bool
	removed map[string]map[int]bool
}

// ParseDiffLineMap builds the line map for a unified diff.
func ParseDiffLineMap(diff []byte) (DiffLineMap, error) {
	return DiffLineMap{}, errNotImplemented
}

// IsAddedLine reports whether head-side line of file was added by the diff.
func (m DiffLineMap) IsAddedLine(file string, line int) bool { return false }

// AddedLines returns the head-side line numbers the diff added to file.
func (m DiffLineMap) AddedLines(file string) []int { return nil }

// RemovedLines returns the base-side line numbers the diff removed from file.
func (m DiffLineMap) RemovedLines(file string) []int { return nil }

// Files returns every path the diff touches, head-side where one exists.
func (m DiffLineMap) Files() []string { return nil }
