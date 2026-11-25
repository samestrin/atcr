//go:build unix

package registry

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TD persona_stamp.go:65 — both stamp reads followed the path and read it
// whole, so a tracked stamp that is a FIFO or a symlink to /dev/zero made
// `atcr doctor` block or read forever. Each is now damaged and both reads
// return.
func TestPersonaStamp_NonRegularStampDoesNotBlock(t *testing.T) {
	for name, create := range map[string]func(t *testing.T, path string){
		"fifo": func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Skipf("mkfifo unsupported: %v", err)
			}
		},
		"symlink to /dev/zero": func(t *testing.T, path string) {
			if _, err := os.Stat("/dev/zero"); err != nil {
				t.Skipf("no /dev/zero: %v", err)
			}
			require.NoError(t, os.Symlink("/dev/zero", path))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			create(t, filepath.Join(dir, PersonaStampFile))

			type result struct {
				damaged, ok bool
			}
			done := make(chan result, 1)
			go func() {
				damaged := PersonaStampDamaged(dir)
				_, ok := ReadPersonaStamp(dir)
				done <- result{damaged, ok}
			}()
			select {
			case r := <-done:
				require.True(t, r.damaged)
				require.False(t, r.ok)
			case <-time.After(5 * time.Second):
				t.Fatal("a stamp read blocked on a non-regular stamp")
			}
		})
	}
}

// TD persona_stamp.go:110 (row 39c45271806eba52) — readPersonaStampFile
// Lstats the stamp and then opens it, so a writer that swaps in a FIFO (or a
// symlink to one) between the two made the open block and hung `atcr doctor`
// before the identity check could run. The swap is now damaged and both reads
// return.
func TestPersonaStamp_SwapToFIFOAfterLstatDoesNotBlock(t *testing.T) {
	for name, viaSymlink := range map[string]bool{"fifo": false, "symlink to fifo": true} {
		t.Run(name, func(t *testing.T) {
			swapToFIFOAfterLstat(t, syscall.Mkfifo, viaSymlink)
		})
	}
}

// A unix without mkfifo skips the swap test rather than failing it: the hook
// ran on the reader goroutine, so its t.Skipf ended only that goroutine and
// the test reported a false 5s "blocked on a FIFO" timeout.
func TestPersonaStamp_SwapToFIFOSkipsWithoutMkfifo(t *testing.T) {
	noMkfifo := func(string, uint32) error { return syscall.ENOSYS }
	for name, viaSymlink := range map[string]bool{"fifo": false, "symlink to fifo": true} {
		var sub *testing.T
		t.Run(name, func(t *testing.T) {
			sub = t
			swapToFIFOAfterLstat(t, noMkfifo, viaSymlink)
		})
		require.True(t, sub.Skipped(), "%s: want a skip when mkfifo fails", name)
	}
}

func swapToFIFOAfterLstat(t *testing.T, mkfifo func(string, uint32) error, viaSymlink bool) {
	swap := func(t *testing.T, _, path string) {
		if err := mkfifo(path, 0o600); err != nil {
			t.Skipf("mkfifo unsupported: %v", err)
		}
	}
	if viaSymlink {
		swap = func(t *testing.T, dir, path string) {
			fifo := filepath.Join(dir, "fifo")
			if err := mkfifo(fifo, 0o600); err != nil {
				t.Skipf("mkfifo unsupported: %v", err)
			}
			require.NoError(t, os.Symlink("fifo", path))
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, PersonaStampFile)
	require.NoError(t, os.WriteFile(path, []byte(`{"files":{}}`), 0o600))

	orig := personaStampLstat
	t.Cleanup(func() { personaStampLstat = orig })
	personaStampLstat = func(name string) (os.FileInfo, error) {
		info, err := orig(name)
		if err == nil && info.Mode().IsRegular() {
			require.NoError(t, os.Remove(name))
			swap(t, dir, name)
		}
		return info, err
	}

	done := make(chan bool, 1)
	go func() { done <- PersonaStampDamaged(dir) }()
	select {
	case damaged := <-done:
		require.True(t, damaged)
	case <-time.After(5 * time.Second):
		// Open the FIFO for writing so the blocked reader returns.
		if w, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
		t.Fatal("a stamp read blocked on a FIFO swapped in after the Lstat")
	}
}
