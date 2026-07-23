package main

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
		if info, err := os.Lstat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
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
