package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// JailError is a structured path-jail rejection. Path is the original,
// unresolved input (never the resolved path); Reason is one of the fixed
// rejection reasons. It satisfies error and is extractable via errors.As.
type JailError struct {
	Path   string
	Reason string
}

func (e *JailError) Error() string {
	return fmt.Sprintf("path jail: %s: %s", e.Reason, e.Path)
}

// Jail confines tool file access to a single snapshot root. Resolve is the only
// way in; it rejects absolute paths, ".." escapes, out-of-root symlinks, and
// any ".git" path component. Jail is safe for concurrent reads.
type Jail struct {
	root string // canonical (EvalSymlinks'd) absolute root
}

// NewJail canonicalizes root via EvalSymlinks at construction. This is the
// critical macOS invariant: temp dirs live under /var -> /private/var, so a
// prefix check against a non-canonical root would false-reject in-root files.
func NewJail(root string) (*Jail, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("jail: cannot absolutize root %s: %w", root, err)
	}
	canon, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("jail: cannot canonicalize root %s: %w", root, err)
	}
	return &Jail{root: canon}, nil
}

// Root returns the canonical jail root.
func (j *Jail) Root() string { return j.root }

// Resolve validates rel and returns the canonical absolute path it maps to
// inside the jail. The pipeline order is fixed (AC 03-01): empty/NUL, absolute,
// Clean + lexical escape, .git component, Join, EvalSymlinks (existing prefix),
// prefix check.
func (j *Jail) Resolve(rel string) (string, error) {
	if rel == "" {
		return "", &JailError{Path: rel, Reason: "empty path not allowed"}
	}
	if strings.ContainsRune(rel, 0) {
		return "", &JailError{Path: rel, Reason: "path contains NUL byte"}
	}
	if filepath.IsAbs(rel) {
		return "", &JailError{Path: rel, Reason: "absolute path not allowed"}
	}

	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", &JailError{Path: rel, Reason: "path escapes snapshot root"}
	}

	// Match ".git" case-insensitively: macOS/Windows default filesystems are
	// case-insensitive, so ".GIT/config" would otherwise resolve to the real
	// .git directory and bypass this block, exposing repository internals.
	for _, seg := range strings.Split(clean, string(os.PathSeparator)) {
		if strings.EqualFold(seg, ".git") {
			return "", &JailError{Path: rel, Reason: "access to .git directory not allowed"}
		}
	}

	candidate := filepath.Join(j.root, clean)
	resolved, err := resolveExisting(candidate)
	if err != nil {
		return "", &JailError{Path: rel, Reason: "cannot resolve path: " + err.Error()}
	}

	if resolved != j.root && !strings.HasPrefix(resolved, j.root+string(os.PathSeparator)) {
		return "", &JailError{Path: rel, Reason: "symlink target escapes snapshot root"}
	}
	return resolved, nil
}

// resolveExisting EvalSymlinks the longest existing ancestor of abs and re-joins
// the not-yet-existing trailing components. This resolves symlinks in the part
// of the path that exists (so an escaping symlink is caught) while tolerating a
// target file that has not been created.
func resolveExisting(abs string) (string, error) {
	rest := ""
	cur := abs
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if rest == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil // nothing along the path exists
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
