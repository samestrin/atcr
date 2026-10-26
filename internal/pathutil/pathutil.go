// Package pathutil holds the `~` display pair: RelHome renders a path with the
// home-directory prefix replaced by "~", and ExpandHome is its inverse. Both take
// the home directory as a resolver argument rather than calling os.UserHomeDir,
// so callers keep their own test seam and home is only looked up when needed.
// Stdlib-only leaf; imports no internal package.
package pathutil

import (
	"errors"
	"path/filepath"
	"strings"
)

// errNoHome is returned by ExpandHome when the resolver succeeds but yields an
// empty home, which would otherwise turn "~/x" into the relative path "x".
var errNoHome = errors.New("home directory is empty")

// RelHome renders path relative to the directory home returns: a path under home
// renders as "~/rel", home itself as "~", and anything outside home (or when home
// errors or is empty) falls back to the verbatim path, never a broken "~/../.."
// substitution. The "~" form is ALWAYS forward-slash (~/rel) on every platform,
// because it feeds cross-platform consumers that parse that documented shape.
func RelHome(path string, home func() (string, error)) string {
	dir, err := home()
	if err != nil || dir == "" {
		return path
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~/" + filepath.ToSlash(rel)
}

// ExpandHome replaces a leading "~" or "~/" with the directory home returns. Any
// other path, including "~user/x", is returned unchanged without calling home.
// It returns an error if home is needed and errors or is empty.
func ExpandHome(path string, home func() (string, error)) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	dir, err := home()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errNoHome
	}
	if path == "~" {
		return dir, nil
	}
	return filepath.Join(dir, path[2:]), nil
}
