//go:build integration && darwin

package sandbox

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.23.1 — kernel-level proof for the measured profile narrowings. The
// shape of the narrowed rules is pinned in oslevel_profile_test.go; only a real
// sandbox-exec run shows the kernel refuses what the rule leaves out. Reuses
// darwinIntegrationBackend and skipOrFail from
// oslevel_profile_darwin_integration_test.go, which this epic does not edit.

func TestIntegration_OSLevelDarwin_FingerprintSysctlsAreUnreadable(t *testing.T) {
	// TD-013 / T1. hw.model and kern.uuid were readable under the unfiltered
	// `(allow sysctl-read)` and are not in darwinSysctlReadNames. The host-side
	// value is the control: its absence from the sandboxed output discriminates
	// "denied" from "read but happened to print nothing useful".
	b := darwinIntegrationBackend(t)

	for _, name := range []string{"hw.model", "kern.uuid"} {
		t.Run(name, func(t *testing.T) {
			hostOut, err := exec.Command("/usr/sbin/sysctl", "-n", name).Output()
			host := strings.TrimSpace(string(hostOut))
			if err != nil || host == "" {
				skipOrFail(t, "%s is not readable on the host, so its absence inside the sandbox proves nothing: %v", name, err)
				return
			}

			res, err := b.Run(context.Background(), RunSpec{
				Command:     []string{"/bin/sh", "-c", "/usr/sbin/sysctl -n " + name + " 2>&1"},
				SnapshotDir: t.TempDir(),
				Timeout:     10 * time.Second,
			})
			require.NoError(t, err, "output: %s", res.Output)
			assert.NotContains(t, res.Output, host,
				"%s leaked into the sandbox — the sysctl-read narrowing is not enforced; output: %s", name, res.Output)
		})
	}
}

func TestIntegration_OSLevelDarwin_MeasuredSysctlsStillResolve(t *testing.T) {
	// The positive control for the test above: a profile denying every sysctl
	// would pass it vacuously and break every shell (the measurement's deny pass
	// failed 14 of 16 proofs). uname(3) reads kern.ostype, kern.osrelease,
	// kern.version and hw.machine, all in darwinSysctlReadNames.
	b := darwinIntegrationBackend(t)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"/bin/sh", "-c", "uname -s -r -m 2>&1"},
		SnapshotDir: t.TempDir(),
		Timeout:     10 * time.Second,
	})
	require.NoError(t, err, "output: %s", res.Output)
	require.Equal(t, 0, res.ExitCode, "uname must run under the narrowed profile; output: %s", res.Output)
	assert.Contains(t, res.Output, "Darwin", "kern.ostype must resolve inside the sandbox; output: %s", res.Output)
}
