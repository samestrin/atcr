package debt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	readmeLockDir   = ".planning/.locks/td-readme.lock"
	readmeLockOwner = "owner.txt"
	readmeLockWait  = 60 * time.Second
	readmeLockStale = 300 * time.Second
	readmeLockSleep = 100 * time.Millisecond
)

// withReadmeLock acquires the shared README lock used by the TD tooling and
// calls fn while holding it. It implements the same mkdir-based protocol as the
// resolve-td and group_td skills so cross-process / cross-session writes are
// serialized.
//
// repoRoot is the repository root directory. The lock is stored under
// <repoRoot>/.planning/.locks/td-readme.lock, matching the skill-level
// protocol exactly.
func withReadmeLock(repoRoot, session string, fn func() error) error {
	lockDir := filepath.Join(repoRoot, readmeLockDir)
	ownerFile := filepath.Join(lockDir, readmeLockOwner)

	// Ensure the parent .planning/.locks directory exists; the lock directory
	// itself is created atomically below.
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o755); err != nil {
		return fmt.Errorf("create lock parent directories: %w", err)
	}

	deadline := time.Now().Add(readmeLockWait)
	for {
		err := os.Mkdir(lockDir, 0o755)
		if err == nil {
			// Acquired. Write owner metadata and ensure cleanup.
			epoch := time.Now().Unix()
			_ = os.WriteFile(ownerFile, []byte(fmt.Sprintf("session=%s|epoch=%d", session, epoch)), 0o644)
			defer func() { _ = os.RemoveAll(lockDir) }()
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire README lock: %w", err)
		}

		// Lock is held. Check for stale owner before waiting.
		if data, err := os.ReadFile(ownerFile); err == nil {
			if e := parseOwnerEpoch(data); e > 0 {
				if time.Since(time.Unix(e, 0)) > readmeLockStale {
					_ = os.RemoveAll(lockDir)
					continue
				}
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout acquiring README lock at %s", lockDir)
		}
		time.Sleep(readmeLockSleep)
	}
}

func parseOwnerEpoch(data []byte) int64 {
	s := strings.TrimSpace(string(data))
	const prefix = "epoch="
	i := strings.Index(s, prefix)
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(s[i+len(prefix):], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// repoRootFromReadme returns the repository root for a README path. It walks up
// the path looking for a .planning directory; if none is found, it falls back
// to the directory containing the README so tests without a full .planning tree
// still lock relative to their temp directory.
func repoRootFromReadme(readmePath string) (string, error) {
	abs, err := filepath.Abs(readmePath)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if filepath.Base(dir) == ".planning" {
			return filepath.Dir(dir), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(abs), nil
		}
		dir = parent
	}
}
