package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
}

// NewSnapshotManager returns a manager rooted at repoRoot (the main worktree).
func NewSnapshotManager(repoRoot string) *SnapshotManager {
	return &SnapshotManager{repoRoot: repoRoot}
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
	leaf := filepath.Join(base, head)

	if err := m.worktreeAdd(gitPath, leaf, head); err != nil {
		// A stale registration from a crashed run can block the add; prune and retry once.
		_, _ = m.git(gitPath, "worktree", "prune")
		if err2 := m.worktreeAdd(gitPath, leaf, head); err2 != nil {
			_ = os.RemoveAll(base)
			return "", noop, fmt.Errorf("snapshot: failed to create worktree for %s: %w", head, err2)
		}
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			// Refuse to remove anything outside the temp dir we created.
			if !strings.HasPrefix(base, filepath.Clean(os.TempDir())) {
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
// (never shell-interpolated) and returns trimmed stdout.
func (m *SnapshotManager) git(gitPath string, args ...string) (string, error) {
	cmd := exec.Command(gitPath, args...)
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
