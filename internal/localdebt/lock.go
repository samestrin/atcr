package localdebt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	lockSubdir = ".lock"
	lockOwner  = "owner.txt"
	lockWait   = 10 * time.Second
	lockStale  = 30 * time.Second
	lockSleep  = 10 * time.Millisecond
)

// withLock acquires a cross-process lock directory under dir and executes fn.
func withLock(dir, session string, fn func() error) error {
	lockDir := filepath.Join(dir, lockSubdir)
	ownerFile := filepath.Join(lockDir, lockOwner)

	// Ensure the parent store directory exists
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating localdebt dir for lock: %w", basePathErr(err))
	}

	deadline := time.Now().Add(lockWait)
	for {
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			// Acquired. Write owner metadata.
			epoch := time.Now().Unix()
			_ = os.WriteFile(ownerFile, []byte(fmt.Sprintf("session=%s|epoch=%d", session, epoch)), 0o600)
			defer func() { _ = os.RemoveAll(lockDir) }()
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire localdebt lock: %w", basePathErr(err))
		}

		// Lock is held. Check for stale owner.
		if data, err := os.ReadFile(ownerFile); err == nil {
			if e := parseOwnerEpoch(data); e > 0 {
				if time.Since(time.Unix(e, 0)) > lockStale {
					_ = os.RemoveAll(lockDir)
					continue
				}
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout acquiring localdebt lock at %s", lockDir)
		}
		time.Sleep(lockSleep)
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
