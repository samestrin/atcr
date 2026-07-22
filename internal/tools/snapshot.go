package tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/samestrin/atcr/internal/gitexec"
)

// shaPattern bounds the head argument before it reaches any git command,
// preventing argument injection. Git accepts abbreviated SHAs of 7+ hex digits.
var shaPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// SnapshotManager produces a read-only filesystem root for a resolved head SHA.
// When head == HEAD and the worktree is clean it returns the live worktree
// (fast path); otherwise it creates a temporary detached git worktree and
// returns a cleanup function that removes it (slow path).
type SnapshotManager struct {
	repoRoot string
	addMu    sync.Mutex // serializes worktreeAdd + prune so concurrent callers cannot interleave
}

// NewSnapshotManager returns a manager rooted at repoRoot (the main worktree).
func NewSnapshotManager(repoRoot string) *SnapshotManager {
	return &SnapshotManager{repoRoot: repoRoot}
}

// snapshotCleanupGuard reports whether base is safe to remove: it must be a
// direct child of the OS temp directory. Both sides are canonicalized via
// filepath.EvalSymlinks so a symlinked TMPDIR (macOS /tmp -> /private/tmp, or
// a symlinked $TMPDIR under /var) does not cause the check to spuriously fail
// and leak a worktree.
func snapshotCleanupGuard(base string) bool {
	sep := string(os.PathSeparator)
	canonBase := base
	if r, err := filepath.EvalSymlinks(base); err == nil {
		canonBase = r
	}
	tmpDir := filepath.Clean(os.TempDir())
	if r, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		tmpDir = r
	}
	return strings.HasPrefix(canonBase, tmpDir+sep)
}

// SnapshotFor returns the snapshot root for head, a cleanup function (safe to
// call multiple times; a no-op on the fast path), and an error. Callers must
// defer cleanup immediately.
func (m *SnapshotManager) SnapshotFor(head string) (string, func(), error) {
	noop := func() {}
	if !shaPattern.MatchString(head) {
		return "", noop, fmt.Errorf("snapshot: invalid head SHA: %s", head)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", noop, fmt.Errorf("snapshot: git not found in PATH")
	}

	resolvedHead, err := m.revParse(gitPath, head+"^{commit}")
	if err != nil {
		return "", noop, fmt.Errorf("snapshot: invalid head SHA: %s", head)
	}
	resolvedHEAD, err := m.revParse(gitPath, "HEAD")
	if err != nil {
		return "", noop, fmt.Errorf("snapshot: failed to resolve HEAD: %w", err)
	}

	if resolvedHead == resolvedHEAD {
		clean, err := m.isClean(gitPath)
		if err != nil {
			return "", noop, fmt.Errorf("snapshot: failed to check working tree status: %w", err)
		}
		if clean {
			return m.repoRoot, noop, nil // fast path: live worktree
		}
	}

	base, err := os.MkdirTemp("", "atcr-snapshot-")
	if err != nil {
		return "", noop, fmt.Errorf("snapshot: cannot create temp dir: %w", err)
	}
	// Name the leaf after the resolved full SHA (not the possibly-abbreviated
	// input) for deterministic, collision-resistant worktree registration.
	leaf := filepath.Join(base, resolvedHead)

	m.addMu.Lock()
	addErr := m.worktreeAdd(gitPath, leaf, resolvedHead)
	if addErr != nil {
		// A stale registration from a crashed run can block the add; prune and retry once.
		firstAddErr := addErr
		if _, perr := m.git(gitPath, "worktree", "prune"); perr != nil {
			fmt.Fprintf(os.Stderr, "snapshot: worktree prune failed: %v\n", perr)
		}
		if err2 := m.worktreeAdd(gitPath, leaf, resolvedHead); err2 != nil {
			addErr = errors.Join(firstAddErr, err2)
		} else {
			addErr = nil
		}
	}
	m.addMu.Unlock()
	if addErr != nil {
		_ = os.RemoveAll(base)
		return "", noop, fmt.Errorf("snapshot: failed to create worktree for %s: %w", head, addErr)
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			// Refuse to remove anything outside the temp dir we created.
			if !snapshotCleanupGuard(base) {
				return
			}
			if _, err := m.git(gitPath, "worktree", "remove", "--force", leaf); err != nil {
				fmt.Fprintf(os.Stderr, "snapshot: git worktree remove failed, falling back to os.RemoveAll: %v\n", err)
			}
			if err := os.RemoveAll(base); err != nil {
				fmt.Fprintf(os.Stderr, "snapshot: failed to remove worktree path %s: %v\n", base, err)
			}
		})
	}
	return leaf, cleanup, nil
}

func (m *SnapshotManager) revParse(gitPath, ref string) (string, error) {
	return m.git(gitPath, "rev-parse", "--verify", "-q", ref)
}

func (m *SnapshotManager) isClean(gitPath string) (bool, error) {
	out, err := m.git(gitPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

func (m *SnapshotManager) worktreeAdd(gitPath, leaf, head string) error {
	_, err := m.git(gitPath, "worktree", "add", "--detach", leaf, head)
	return err
}

// git runs a git subcommand in the repo root with explicit argument arrays
// (never shell-interpolated) and returns trimmed stdout. The subprocess is built
// through gitexec so it carries the GIT_CONFIG_NOSYSTEM/GLOBAL hardening against a
// poisoned system/global gitconfig; cmd.Path is pinned to the pre-resolved gitPath
// so the exact binary validated by exec.LookPath in SnapshotFor is the one run.
func (m *SnapshotManager) git(gitPath string, args ...string) (string, error) {
	cmd := gitexec.CommandFn(args...)
	cmd.Path = gitPath
	// gitexec.CommandFn ran exec.LookPath("git") at construction and stored any
	// failure in cmd.Err; clear it so the pinned absolute gitPath is authoritative
	// regardless of whether "git" is still on PATH at call time (matches the
	// pre-migration exec.Command(gitPath, ...) which did no call-time lookup).
	cmd.Err = nil
	cmd.Dir = m.repoRoot
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
