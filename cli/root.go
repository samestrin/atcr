package cli

import (
	"os"
	"path/filepath"
)

// repoRoot returns the repository root directory: the nearest ancestor of the
// current working directory that contains a `.git` or `.atcr` directory. If no
// such marker is found, it falls back to the current working directory so
// commands continue to work when run outside a repo (e.g. tests with an explicit
// path). This helper can be adopted by other subcommands to make atcr
// cwd-independent.
func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		// .git counts as a marker when it is a directory OR a regular FILE: a
		// linked worktree and a submodule both record their root with a .git file,
		// and rejecting those would refuse to find the root in exactly the
		// checkouts where a developer is most likely to be working somewhere other
		// than the main tree — which is what put the debt store's readers and its
		// writer (localdebt.validateRepoRoot, which accepts both forms) on
		// different roots.
		//
		// It stays an Lstat, and a SYMLINK still does not count: os.Stat would
		// follow a .git symlink pointing at an arbitrary directory and let it pass
		// as a repo root. Widening to regular files does not widen to links.
		if info, err := os.Lstat(filepath.Join(dir, ".git")); err == nil &&
			(info.IsDir() || info.Mode().IsRegular()) {
			return dir, nil
		}
		if info, err := os.Lstat(filepath.Join(dir, ".atcr")); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return cwd, nil
}
