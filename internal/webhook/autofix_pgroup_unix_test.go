//go:build unix

package webhook

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// processAlive reports whether pid names a live process. Signal 0 probes for
// existence without delivering a signal. A zombie, an exited process nobody
// has waited for yet (an orphan whose new parent does not reap), counts as
// gone: it runs nothing and holds nothing open.
func processAlive(pid int) bool {
	if syscall.Kill(pid, syscall.Signal(0)) == syscall.ESRCH {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // no procfs: trust the signal probe
	}
	// The state follows the parenthesised command name: "pid (comm) S ...".
	_, rest, ok := strings.Cut(string(stat), ") ")
	return !ok || !strings.HasPrefix(rest, "Z")
}

// TestAutoFixRunner_CancelReapsGrandchildOutlivingAtcr is a child that exits on
// SIGTERM while a grandchild it started lives on: the grandchild shares the
// child's process group and is killed with it once the SIGTERM grace is over.
func TestAutoFixRunner_CancelReapsGrandchildOutlivingAtcr(t *testing.T) {
	f := newAutoFixFixture(t)
	t.Setenv(testPushTokenEnv, testPushToken)
	old := autoFixKillGrace
	autoFixKillGrace = 300 * time.Millisecond
	t.Cleanup(func() { autoFixKillGrace = old })
	// The grandchild's output goes elsewhere, so it holds none of the child's
	// pipes open and Run is not kept waiting on them.
	stub := newAutoFixStub(t, `sleep 30 >/dev/null 2>&1 &
echo $! > "$(dirname "$0")/rec/grandchild"
touch started
wait`)
	logs := &logLines{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if hasStarted(f) {
				return
			}
		}
	}()
	err := newAutoFixRunner(f, stub, logs).Run(ctx, AutoFixTarget{Repo: "acme/widget", Number: 7, Base: "feature/login"})
	assert.ErrorIs(t, err, context.Canceled)

	grandchild, perr := strconv.Atoi(strings.TrimSpace(stub.read(t, "grandchild")))
	require.NoError(t, perr, "the stub recorded its grandchild's pid")
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })

	require.Eventually(t, func() bool { return !processAlive(grandchild) }, 2*time.Second, 20*time.Millisecond,
		"grandchild (pid %d) outlived the cancelled atcr child", grandchild)
	assertNothingLeft(t, f)
}
