package astgroup

import (
	"errors"
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
	host     *Host
	root     string
	readFile func(string) ([]byte, error)
	ownsHost bool

	mu    sync.Mutex
	cache map[string]*parsedFile // keyed by finding.File
}

type parsedFile struct {
	tree Node
	ok   bool
}

// isPermanentReadError reports whether an error from reading a source file is
// expected to persist across retries. Not-exist, permission-denied, and
// containment/path errors are permanent; transient resource contention is not.
func isPermanentReadError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) || os.IsPermission(err) {
		return true
	}
	// Path errors from canonicalPath are permanent containment failures.
	var pErr *os.PathError
	if errors.As(err, &pErr) && (pErr.Err == os.ErrInvalid) {
		return true
	}
	return false
}

// NewGrouper builds a Grouper rooted at root: relative finding paths are resolved
// against root before reading. Pass the reconcile Options.Root.
//
// If a Host is supplied it is borrowed (not closed by this Grouper); callers
// typically pass SharedHost() so parser instances are reused across reconciles.
// If no Host is supplied, a fresh Host is created and owned by this Grouper, and
// Close releases it. This backward-compatible default keeps existing tests and
// callers working without change.
func NewGrouper(root string, host ...*Host) *Grouper {
	var h *Host
	owns := false
	if len(host) > 0 && host[0] != nil {
		h = host[0]
	} else {
		h = NewHost()
		owns = true
	}
	return &Grouper{host: h, root: root, readFile: os.ReadFile, ownsHost: owns, cache: map[string]*parsedFile{}}
}

// Close releases this Grouper's resources. If the Grouper owns its Host, the
// underlying wazero runtime is closed; borrowed Hosts are left open so their
// compiled-parser cache can be reused across reconciles.
func (g *Grouper) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cache = nil
	if g.ownsHost {
		return g.host.Close()
	}
	return nil
}

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
	if err != nil {
		return "", false
	}
	// Normalize to forward slashes so the escape check is identical on Windows
	// (where filepath.Rel yields backslash-separated paths) and Unix.
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
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
	src, err := g.readFile(path)
	if err != nil {
		// Only cache permanent failures. Transient I/O errors (EAGAIN, EMFILE,
		// a file briefly locked) should be retried on the next GroupKey call.
		if isPermanentReadError(err) {
			return pf
		}
		// Do not cache the negative result; leave the entry absent so the next
		// call retries the read.
		delete(g.cache, file)
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
