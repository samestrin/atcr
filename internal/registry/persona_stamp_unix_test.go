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
