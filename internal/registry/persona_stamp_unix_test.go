//go:build unix

package registry

import (
	"io"
	"os"
	"path/filepath"
	"strings"
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

// TD persona_stamp.go:116,127 (rows 705fc6fc10e64368, d5e5c16e9630f7a5,
// c620143f636c9b62) — a symlink swapped in after the Lstat failed the open
// with a bare ELOOP that did not say why, and nothing pinned O_NOFOLLOW: the
// existing swap test only links to a FIFO. Here the link points at a regular
// file holding a valid stamp, so a follow would read it. The reader must
// refuse it at the open (ELOOP, not the later identity check) and name a
// possible swap after the Lstat.
func TestPersonaStamp_SwapToSymlinkAfterLstatIsRefused(t *testing.T) {
	t.Run("symlink to a regular file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, PersonaStampFile)
		require.NoError(t, os.WriteFile(path, []byte(`{"files":{}}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "other"), []byte(`{"files":{"a":"b"}}`), 0o600))

		hook := func(name string) (os.FileInfo, error) {
			info, err := os.Lstat(name)
			if err == nil && info.Mode().IsRegular() {
				if rerr := os.Remove(name); rerr != nil {
					t.Errorf("remove the stamp: %v", rerr)
				} else if serr := os.Symlink("other", name); serr != nil {
					t.Errorf("swap the stamp for a symlink: %v", serr)
				}
			}
			return info, err
		}

		data, present, err := readPersonaStampFileWith(path, hook)
		require.True(t, present)
		require.Nil(t, data)
		require.ErrorIs(t, err, syscall.ELOOP, "O_NOFOLLOW must refuse the link at the open")
		require.ErrorContains(t, err, "possible swap after lstat")
		require.ErrorContains(t, err, path)
	})
}

// TD persona_stamp.go:127 (rows 630bd41174d656f2, acb6ae90ded75dbf) — the
// stamp is opened with O_NONBLOCK and the flag stays set for the read. On a
// regular file it changes nothing, so a regular stamp opened with the same
// flags reads whole, directly and through readPersonaStampFile.
func TestPersonaStamp_NonBlockOpenReadsRegularStamp(t *testing.T) {
	require.NotZero(t, openNoFollowNonBlock&syscall.O_NONBLOCK, "the stamp open must carry O_NONBLOCK")
	dir := t.TempDir()
	path := filepath.Join(dir, PersonaStampFile)
	// Bigger than one read buffer, so a short nonblocking read would show.
	want := []byte(`{"files":{"x":"` + strings.Repeat("y", 256<<10) + `"}}`)
	require.NoError(t, os.WriteFile(path, want, 0o600))

	f, err := os.OpenFile(path, os.O_RDONLY|openNoFollowNonBlock, 0)
	require.NoError(t, err)
	got, err := io.ReadAll(f)
	require.NoError(t, f.Close())
	require.NoError(t, err)
	require.Equal(t, want, got)

	data, present, err := readPersonaStampFile(path)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, want, data)
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

// swapToFIFOAfterLstat makes the FIFO and any skip on the test goroutine,
// before the reader starts: the Lstat hook runs on the reader goroutine, where
// t.Skip and require would end only that goroutine. The hook just swaps and
// reports a failed swap with t.Errorf.
func swapToFIFOAfterLstat(t *testing.T, mkfifo func(string, uint32) error, viaSymlink bool) {
	dir := t.TempDir()
	path := filepath.Join(dir, PersonaStampFile)
	require.NoError(t, os.WriteFile(path, []byte(`{"files":{}}`), 0o600))
	fifo := filepath.Join(dir, "fifo")
	if err := mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	swap := func(name string) error { return os.Rename(fifo, name) }
	if viaSymlink {
		swap = func(name string) error {
			if err := os.Remove(name); err != nil {
				return err
			}
			return os.Symlink("fifo", name)
		}
	}

	hook := func(name string) (os.FileInfo, error) {
		info, err := os.Lstat(name)
		if err == nil && info.Mode().IsRegular() {
			if serr := swap(name); serr != nil {
				t.Errorf("swap the stamp for a FIFO: %v", serr)
			}
		}
		return info, err
	}

	// The hook goes straight to the read, so no package-level seam is swapped
	// and a parallel test cannot see it. A swapped stamp is present with err
	// set, which is what PersonaStampDamaged reports as damaged.
	type result struct {
		present bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		_, present, err := readPersonaStampFileWith(path, hook)
		done <- result{present, err}
	}()
	select {
	case r := <-done:
		require.True(t, r.present)
		require.Error(t, r.err)
	case <-time.After(5 * time.Second):
		// Release the blocked reader before failing, so it does not outlive
		// the TempDir. A write-only nonblocking open
		// fails with ENXIO; O_RDWR on a FIFO succeeds without a reader, and
		// closing it right away hands the reader EOF.
		if w, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			_ = w.Close()
		}
		<-done
		t.Fatal("a stamp read blocked on a FIFO swapped in after the Lstat")
	}
}
