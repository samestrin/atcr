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
		// .git must be a DIRECTORY here, and the strictness is load-bearing for
		// this helper's eight consumers (config, telemetry consent, history,
		// audit, resume, review): widening it to a .git FILE would treat a
		// submodule or linked worktree as its own atcr repo and silently relocate
		// their state — including a recorded telemetry opt-out — out of the parent
		// repo. The debt store needs the broader rule to agree with its writer and
		// carries its own walk (debtRepoRoot, cli/debt.go) for exactly that reason.
		//
		// Lstat, not Stat: os.Stat would follow a .git symlink pointing at an
		// arbitrary directory and let it pass as a repo root.
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
