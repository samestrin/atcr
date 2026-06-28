package astgroup

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/samestrin/atcr/reconcile"
)

// Grouper implements reconcile.Grouper using AST isomorphism. For each finding it
// resolves the finding's file to a parser language, parses the source (once per
// file, cached) into a structural tree via the wazero-backed Host, maps the
// finding's line to its smallest covering AST node, and returns a file-scoped
// Merkle hash of that node as the grouping key. Findings that share a key are the
// "same logical block" and cluster together regardless of line drift.
//
// A returned empty key means "fall back to line proximity": the finding is
// file-level (Line <= 0), its language has no parser, the file is missing or
// unparseable, or no node covers the line.
type Grouper struct {
	host *Host
	root string

	mu    sync.Mutex
	cache map[string]*parsedFile // keyed by finding.File
}

type parsedFile struct {
	tree Node
	ok   bool
}

// NewGrouper builds a Grouper rooted at root: relative finding paths are resolved
// against root before reading. Pass the reconcile Options.Root. Call Close to
// release the underlying wazero runtime.
func NewGrouper(root string) *Grouper {
	return &Grouper{host: NewHost(), root: root, cache: map[string]*parsedFile{}}
}

// Close releases the wazero runtime.
func (g *Grouper) Close() error { return g.host.Close() }

// resolvePath maps a finding's File to an on-disk path and confirms it stays
// within root. It refuses paths that escape root — via "../" or an absolute path
// outside it — so a hostile finding.File cannot turn the grouper into a
// file-existence oracle for arbitrary locations. An empty root is treated as the
// current working directory rather than disabling the guard, so containment
// always applies. ok is false when the path is rejected.
func (g *Grouper) resolvePath(file string) (string, bool) {
	root := g.root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	p := file
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, file)
	}
	p = filepath.Clean(p)

	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

// GroupKey returns the AST-isomorphism key for f, or "" to fall back to proximity.
func (g *Grouper) GroupKey(f reconcile.Finding) string {
	if f.Line <= 0 || f.File == "" {
		return ""
	}
	// Normalize the path so two reviewers citing the same file with different
	// spellings ("x.go" vs "./x.go") share a cache entry and group key.
	file := filepath.Clean(f.File)
	lang := LanguageForExt(strings.ToLower(filepath.Ext(file)))
	if lang == "" {
		return ""
	}
	pf := g.treeFor(file, lang)
	if !pf.ok {
		return ""
	}
	block, addr, ok := CoveringBlock(pf.tree, f.Line)
	if !ok {
		return ""
	}
	// Key = file + structural address of the covering block + its Merkle hash.
	// The address (drift-invariant, sibling-distinguishing) prevents two
	// identically-shaped blocks in different scopes from colliding; the Merkle
	// hash folds in the block's full structure per the epic's design. File-scoped
	// so identical structures in different files never collide.
	return file + "\x00" + addr + "\x00" + MerkleHash(block)
}

// treeFor returns the parsed tree for file, parsing+caching on first use. A parse
// or read failure is cached as a negative result so a bad file is not re-read for
// every finding in it.
func (g *Grouper) treeFor(file, lang string) *parsedFile {
	g.mu.Lock()
	defer g.mu.Unlock()

	if pf, ok := g.cache[file]; ok {
		return pf
	}

	pf := &parsedFile{}
	g.cache[file] = pf // cache the (initially negative) result; updated below on success

	path, ok := g.resolvePath(file)
	if !ok {
		return pf // path escapes root: refuse to read, fall back to proximity
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return pf
	}
	parser, err := g.host.Parser(lang)
	if err != nil {
		return pf
	}
	tree, err := parser.Parse(src)
	if err != nil {
		return pf
	}
	pf.tree = tree
	pf.ok = true
	return pf
}
