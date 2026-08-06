package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// repoRoot returns the repository root directory: the nearest ancestor of the
// current working directory that contains a `.git` or `.atcr` directory — or,
// for a linked worktree, the MAIN checkout that worktree belongs to. If no
// such marker is found, it falls back to the current working directory so
// commands continue to work when run outside a repo (e.g. tests with an
// explicit path). This helper can be adopted by other subcommands to make atcr
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
		// submodule as its own atcr repo and silently relocate its state —
		// including a recorded telemetry opt-out — out of the parent repo. The
		// debt store needs the broader rule to agree with its writer and
		// carries its own walk (debtRepoRoot, cli/debt.go) for exactly that
		// reason.
		//
		// The ONE exception is a genuine linked worktree: its .git file's
		// gitdir target contains git's own commondir marker (absent from a
		// submodule's .git/modules/<name> gitdir), which resolves back to the
		// main checkout's .git directory. Returning the main checkout — rather
		// than falling through and landing on whatever subdirectory the
		// command ran from — converges every worktree of one repository on the
		// same canonical .atcr/config.yaml, telemetry consent, history, audit
		// and resume root. A submodule's gitfile has no commondir, so it keeps
		// the walk-past behavior unchanged.
		//
		// Lstat, not Stat: os.Stat would follow a .git symlink pointing at an
		// arbitrary directory and let it pass as a repo root.
		if info, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			if info.IsDir() {
				return dir, nil
			}
			if info.Mode().IsRegular() {
				if root := worktreeMainRoot(filepath.Join(dir, ".git")); root != "" {
					return root, nil
				}
			}
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

// worktreeMainRoot resolves the MAIN checkout's root from a linked worktree's
// .git file, or "" when the file is not a linked-worktree pointer. The file
// carries a `gitdir: <main>/.git/worktrees/<name>` line, and that target
// directory's `commondir` file — git's own linked-worktree marker, which a
// submodule's .git/modules/<name> gitdir does NOT have — names the main .git
// directory relative to the target. Any parse or existence failure yields ""
// so the caller keeps the walk-past-submodule behavior: a malformed pointer is
// a reason to distrust the file, never to guess a root.
func worktreeMainRoot(gitFile string) string {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(gitFile), target)
	}
	common, err := os.ReadFile(filepath.Join(target, "commondir"))
	if err != nil {
		return "" // no commondir marker: a submodule gitdir, not a linked worktree
	}
	mainGit := strings.TrimSpace(string(common))
	if !filepath.IsAbs(mainGit) {
		mainGit = filepath.Join(target, mainGit)
	}
	mainGit = filepath.Clean(mainGit)
	if info, err := os.Stat(mainGit); err != nil || !info.IsDir() {
		return ""
	}
	return filepath.Dir(mainGit)
}
