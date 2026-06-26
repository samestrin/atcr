package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubResolver is a minimal jail that resolves nothing (exec tools take no path
// args) and reports a fixed root.
type stubResolver struct{ root string }

func (s stubResolver) Resolve(string) (string, error) { return s.root, nil }
func (s stubResolver) Root() string                   { return s.root }

// stubBackend records the last RunSpec it saw and returns a canned result.
type stubBackend struct {
	last   sandbox.RunSpec
	result sandbox.RunResult
	runErr error
}

func (b *stubBackend) Name() string                    { return "stub" }
func (b *stubBackend) Preflight(context.Context) error { return nil }
func (b *stubBackend) Run(_ context.Context, s sandbox.RunSpec) (sandbox.RunResult, error) {
	b.last = s
	return b.result, b.runErr
}

func newExecDispatcher(t *testing.T, b sandbox.Backend) *Dispatcher {
	t.Helper()
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	d.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)
	return d
}

func TestExecutionTools_Defs(t *testing.T) {
	defs := ExecutionTools()
	names := map[string]ToolDef{}
	for _, d := range defs {
		names[d.Name] = d
	}
	require.Contains(t, names, "run_tests")
	require.Contains(t, names, "run_script")
	// run_script must require a content arg.
	req, _ := names["run_script"].Parameters["required"].([]string)
	assert.Contains(t, req, "content")
}

func TestDispatcher_NoExecToolsByDefault(t *testing.T) {
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	got := strings.Join(d.RegisteredTools(), ",")
	assert.NotContains(t, got, "run_tests")
	assert.NotContains(t, got, "run_script")
}

func TestDispatcher_EnableExecution_RegistersTools(t *testing.T) {
	d := newExecDispatcher(t, &stubBackend{})
	got := strings.Join(d.RegisteredTools(), ",")
	assert.Contains(t, got, "run_tests")
	assert.Contains(t, got, "run_script")
}

func TestRunTests_Handler_ScopesTargetAndReportsExit(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{Command: "go test ./internal/x", ExitCode: 1, Output: "FAIL: x"}}
	d := newExecDispatcher(t, b)

	res, err := d.Execute(context.Background(), "run_tests", json.RawMessage(`{"target":"./internal/x"}`))
	require.NoError(t, err)
	// The project test command is taken from config and the target is appended.
	assert.Equal(t, []string{"go", "test", "./...", "./internal/x"}, b.last.Command)
	// The snapshot root is mounted read-only as the work tree.
	assert.Equal(t, "/snap", b.last.SnapshotDir)
	// The result surfaces the exit code so the model sees pass/fail.
	assert.Contains(t, res.Content, "exit")
	assert.Contains(t, res.Content, "1")
	assert.Contains(t, res.Content, "FAIL: x")
}

func TestRunTests_Handler_NoTargetRunsFullSuite(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newExecDispatcher(t, b)
	_, err := d.Execute(context.Background(), "run_tests", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"go", "test", "./..."}, b.last.Command)
}

func TestRunScript_Handler_PassesContentAndTimeout(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 2, Output: "boom"}}
	d := newExecDispatcher(t, b)

	res, err := d.Execute(context.Background(), "run_script",
		json.RawMessage(`{"content":"echo hi\nexit 2\n","timeout":5}`))
	require.NoError(t, err)
	assert.Equal(t, "echo hi\nexit 2\n", b.last.Script)
	assert.Equal(t, 5*time.Second, b.last.Timeout)
	assert.Contains(t, res.Content, "2")
	assert.Contains(t, res.Content, "boom")
}

func TestRunScript_Handler_ClampsTimeout(t *testing.T) {
	// execTimeout is the operator's configured per-run budget (30s here). A
	// model-supplied per-call timeout may only SHORTEN a run; an oversized or
	// non-positive value must be floored/capped to execTimeout, never allowed to
	// extend the run past the operator's validated budget.
	for _, tc := range []struct {
		name    string
		timeout int
		want    time.Duration
	}{
		{"oversized capped to execTimeout", 9999, 30 * time.Second},
		{"in-range preserved", 5, 5 * time.Second},
		{"zero floored to execTimeout", 0, 30 * time.Second},
		{"negative floored to execTimeout", -1, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
			d := newExecDispatcher(t, b)
			_, err := d.Execute(context.Background(), "run_script",
				json.RawMessage(fmt.Sprintf(`{"content":"echo hi\n","timeout":%d}`, tc.timeout)))
			require.NoError(t, err)
			assert.Equal(t, tc.want, b.last.Timeout,
				"per-call timeout must be clamped to the dispatcher's execTimeout")
		})
	}
}

func TestRunScript_Handler_RequiresContent(t *testing.T) {
	d := newExecDispatcher(t, &stubBackend{})
	_, err := d.Execute(context.Background(), "run_script", json.RawMessage(`{}`))
	require.Error(t, err, "run_script with no content must be a tool error")
}

func TestRunTests_Handler_RejectsFlaglikeTarget(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newExecDispatcher(t, b)
	for _, target := range []string{"-v", "--output=/tmp/x", "-run=TestX"} {
		_, err := d.Execute(context.Background(), "run_tests",
			json.RawMessage(`{"target":"`+target+`"}`))
		require.Error(t, err, "a target starting with '-' must be rejected as argument injection: %q", target)
		assert.Empty(t, b.last.Command, "a rejected target must not be dispatched to the backend: %q", target)
	}
}

func TestRunScript_Handler_RejectsOversizedContent(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newExecDispatcher(t, b)
	big := strings.Repeat("a", 64*1024+1)
	_, err := d.Execute(context.Background(), "run_script",
		json.RawMessage(`{"content":"`+big+`"}`))
	require.Error(t, err, "run_script content over the size cap must be a tool error")
	assert.Empty(t, b.last.Script, "oversized content must not be dispatched to the backend")
}

func TestExecTools_DisabledWhenNotEnabled(t *testing.T) {
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	_, err := d.Execute(context.Background(), "run_tests", json.RawMessage(`{}`))
	require.Error(t, err, "exec tools must be unknown until EnableExecution is called")
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestDispatcher_EnableExecution_ConcurrentWithExecute(t *testing.T) {
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}

	go func() {
		d.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)
	}()

	// Concurrent Execute must not race with EnableExecution's registration.
	_, _ = d.Execute(context.Background(), "run_tests", json.RawMessage(`{}`))
}
