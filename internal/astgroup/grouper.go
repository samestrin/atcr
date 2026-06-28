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

// canonicalPath returns the symlink-resolved, root-contained on-disk path for
// file. It collapses relative/absolute spellings and symlinks that point to the
// same real file so they share a single cache entry and group key, while still
// rejecting any path that escapes root (including a symlink that points outside
// it). Root itself is symlink-resolved so a root like /tmp on macOS (/private/tmp)
// does not falsely reject contained files.
func (g *Grouper) canonicalPath(file string) (string, bool) {
	root := g.root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)
	rootReal, err := filepath.EvalSymlinks(root)
	if err == nil {
		root = rootReal
	}

	p := file
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, file)
	}
	p = filepath.Clean(p)

	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		// If the symlink cannot be resolved, fall back to the lexically
		// resolved path rather than silently returning an empty key.
		real = p
	}

	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return real, true
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
	path, ok := g.canonicalPath(file)
	if !ok {
		return ""
	}
	pf := g.treeFor(path, lang)
	if !pf.ok {
		return ""
	}
	block, addr, ok := CoveringBlock(pf.tree, f.Line)
	if !ok {
		return ""
	}
	// Key = canonical file path + structural address of the covering block + its
	// Merkle hash. The address (drift-invariant, sibling-distinguishing) already
	// uniquely identifies the node within the file, so the Merkle hash is a
	// defensive cross-check of the address scheme rather than load-bearing for
	// grouping. File-scoped so identical structures in different files never
	// collide; canonical path collapses symlinks and spelling variants.
	return path + "\x00" + addr + "\x00" + MerkleHash(block)
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

	path, ok := g.canonicalPath(file)
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
