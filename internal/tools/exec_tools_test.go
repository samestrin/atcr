package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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

// execCtx is an exec-eligible context: the per-call gate (Epic 11.1) now refuses
// run_tests/run_script unless the caller affirmatively granted eligibility, so
// handler-behavior tests must dispatch through one.
func execCtx() context.Context { return WithExecEligibility(context.Background(), true) }

// TestEnableExecution_EveryExecToolIsGated is the Epic 11.2 structural invariant:
// TestEnableExecution_EveryExecToolIsGated is the Epic 11.2 structural invariant:
// ExecutionTools() is the authoritative registry of sandbox-reaching tools.
// The enforcement is already structural in three layers: (1) Execute()
// (dispatch.go:225) refuses any execGated tool unless execEligible(ctx) is true;
// (2) registerExec (dispatch.go:186-192) is the sole writer of the unexported
// execTools map; (3) runInSandbox/execBackend (exec_tools.go:212,
// dispatch.go:80) are unexported, so no handler registered through the public
// API can reach the sandbox. Every execution tool offered to agents must,
// once wired by EnableExecution, be present in execTools AND backed by a
// registered handler. The test asserts the converse too: no execTools entry
// exists without a backing handler (no orphan gates), and every execTools
// entry is listed in ExecutionTools().
func TestEnableExecution_EveryExecToolIsGated(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newExecDispatcher(t, b)

	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, def := range ExecutionTools() {
		assert.True(t, d.execTools[def.Name], "exec tool %q must be gated in execTools", def.Name)
		_, hasHandler := d.handlers[def.Name]
		assert.True(t, hasHandler, "exec tool %q must have a registered handler", def.Name)
	}
	for name := range d.execTools {
		_, hasHandler := d.handlers[name]
		assert.True(t, hasHandler, "execTools gate %q has no backing handler (orphan gate)", name)
		found := false
		for _, def := range ExecutionTools() {
			if def.Name == name {
				found = true
				break
			}
		}
		assert.True(t, found, "execTools gate %q must be listed in ExecutionTools()", name)
	}
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

	res, err := d.Execute(execCtx(), "run_tests", json.RawMessage(`{"target":"./internal/x"}`))
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
	_, err := d.Execute(execCtx(), "run_tests", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"go", "test", "./..."}, b.last.Command)
}

func TestRunScript_Handler_PassesContentAndTimeout(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 2, Output: "boom"}}
	d := newExecDispatcher(t, b)

	res, err := d.Execute(execCtx(), "run_script",
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
			_, err := d.Execute(execCtx(), "run_script",
				json.RawMessage(fmt.Sprintf(`{"content":"echo hi\n","timeout":%d}`, tc.timeout)))
			require.NoError(t, err)
			assert.Equal(t, tc.want, b.last.Timeout,
				"per-call timeout must be clamped to the dispatcher's execTimeout")
		})
	}
}

func TestRunScript_Handler_RequiresContent(t *testing.T) {
	d := newExecDispatcher(t, &stubBackend{})
	_, err := d.Execute(execCtx(), "run_script", json.RawMessage(`{}`))
	require.Error(t, err, "run_script with no content must be a tool error")
}

func TestRunTests_Handler_RejectsFlaglikeTarget(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newExecDispatcher(t, b)
	for _, target := range []string{"-v", "--output=/tmp/x", "-run=TestX"} {
		_, err := d.Execute(execCtx(), "run_tests",
			json.RawMessage(`{"target":"`+target+`"}`))
		require.Error(t, err, "a target starting with '-' must be rejected as argument injection: %q", target)
		assert.Empty(t, b.last.Command, "a rejected target must not be dispatched to the backend: %q", target)
	}
}

func TestRunScript_Handler_RejectsOversizedContent(t *testing.T) {
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newExecDispatcher(t, b)
	big := strings.Repeat("a", 64*1024+1)
	_, err := d.Execute(execCtx(), "run_script",
		json.RawMessage(`{"content":"`+big+`"}`))
	require.Error(t, err, "run_script content over the size cap must be a tool error")
	assert.Empty(t, b.last.Script, "oversized content must not be dispatched to the backend")
}

func TestDispatcher_Execute_RefusesExecToolWithoutEligibility(t *testing.T) {
	// AC1: the read-only boundary is STRUCTURAL at dispatch. Even on a shared
	// dispatcher that HAS the exec tools registered (EnableExecution), a caller
	// that was not granted exec eligibility must be refused — the call must never
	// reach the sandbox backend. This covers both the absent-key path and an
	// explicit deny (false) value, which pins default-deny against a
	// presence-vs-value regression.
	cases := []struct {
		name string
		ctx  context.Context
	}{
		{"absent", context.Background()},
		{"explicit_false", WithExecEligibility(context.Background(), false)},
	}
	for _, name := range []string{"run_tests", "run_script"} {
		for _, tc := range cases {
			t.Run(name+"_"+tc.name, func(t *testing.T) {
				b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
				d := newExecDispatcher(t, b)
				_, err := d.Execute(tc.ctx, name, json.RawMessage(`{"content":"echo hi\n","target":"./x"}`))
				require.Error(t, err, "an exec tool named by a non-eligible caller must be a tool error, not an execution")
				var te *ToolError
				assert.ErrorAs(t, err, &te, "refusal must be a non-fatal ToolError, never a panic")
				assert.Empty(t, b.last.Command, "a refused exec call must never reach the backend")
				assert.Empty(t, b.last.Script, "a refused exec call must never reach the backend")
			})
		}
	}
}

func TestDispatcher_Execute_AllowsExecToolWithEligibility(t *testing.T) {
	// Positive control: with eligibility granted in the context, the same call on
	// the same dispatcher runs normally — the gate refuses only the non-eligible.
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d := newExecDispatcher(t, b)
	ctx := WithExecEligibility(context.Background(), true)
	_, err := d.Execute(ctx, "run_tests", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"go", "test", "./..."}, b.last.Command)
}

func TestExecTools_DisabledWhenNotEnabled(t *testing.T) {
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	_, err := d.Execute(context.Background(), "run_tests", json.RawMessage(`{}`))
	require.Error(t, err, "exec tools must be unknown until EnableExecution is called")
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestDispatcher_EnableExecution_ConcurrentExecuteNeverFailsOpen(t *testing.T) {
	// Gate-first ordering invariant (Epic 11.1 hardening): while EnableExecution is
	// registering the exec tools, a concurrent NON-eligible Execute must never reach
	// the backend. With handler-first ordering there is a window where the handler
	// exists but the gate flag is not yet set (fail-OPEN); gate-first closes it.
	// Run under -race with many iterations to exercise the registration window.
	for i := 0; i < 200; i++ {
		d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
		b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
		done := make(chan struct{})
		go func() {
			d.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)
			close(done)
		}()
		// A bare (non-eligible) context: whether the handler is registered yet or not,
		// the call must be either "unknown tool" or an eligibility refusal — never an
		// execution.
		_, _ = d.Execute(context.Background(), "run_tests", json.RawMessage(`{}`))
		<-done
		require.Empty(t, b.last.Command, "a non-eligible exec call must never reach the backend, even mid-registration")
	}
}

func TestDispatcher_Execute_RefusalLoggerNamesTool(t *testing.T) {
	// When an exec-gated tool is refused for a non-eligible caller, the injected
	// RefusalLogger sink must fire with the refused tool name so an operator can see
	// the attempt. With eligibility granted, the same call must NOT fire the sink.
	d := newExecDispatcher(t, &stubBackend{})

	var refused []string
	ctx := WithRefusalLogger(context.Background(), func(tool string) { refused = append(refused, tool) })
	_, err := d.Execute(ctx, "run_tests", json.RawMessage(`{}`))
	require.Error(t, err, "a non-eligible caller must be refused")
	assert.Equal(t, []string{"run_tests"}, refused, "the refusal sink must be invoked naming the refused tool")

	refused = nil
	ctxOK := WithRefusalLogger(WithExecEligibility(context.Background(), true),
		func(tool string) { refused = append(refused, tool) })
	_, err = d.Execute(ctxOK, "run_tests", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Empty(t, refused, "an allowed exec call must not fire the refusal sink")
}

func TestDispatcher_EnableExecution_RaceWithEligibleExecute(t *testing.T) {
	// Eligible-caller companion to ...ConcurrentExecuteNeverFailsOpen: an exec-eligible
	// Execute racing EnableExecution must not data-race on the backend fields
	// (execBackend/execTestCmd/execTimeout). EnableExecution now publishes them under
	// d.mu, so a reader that observes the registered handler also observes the fields.
	// Run under -race to exercise the publish/read window.
	for i := 0; i < 200; i++ {
		d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
		b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
		done := make(chan struct{})
		go func() {
			d.EnableExecution(b, []string{"go", "test", "./..."}, 30*time.Second)
			close(done)
		}()
		// Eligible context: if the handler is registered, the call reaches runInSandbox
		// and reads the backend fields concurrently with EnableExecution's publish.
		_, _ = d.Execute(execCtx(), "run_tests", json.RawMessage(`{}`))
		<-done
	}
}

func TestRunTests_Handler_RequiresTestCmd(t *testing.T) {
	// If EnableExecution is wired with an empty test command, run_tests must fail
	// with a clear configuration error instead of forwarding an empty Command to
	// the sandbox backend.
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d.EnableExecution(b, []string{}, 30*time.Second)
	_, err := d.Execute(execCtx(), "run_tests", json.RawMessage(`{}`))
	require.Error(t, err, "run_tests with an empty test command must be a configuration error")
	var te *ToolError
	assert.ErrorAs(t, err, &te)
	assert.Empty(t, b.last.Command, "an empty test command must not be dispatched to the backend")
}

func TestRunScript_Handler_RequiresExecTimeout(t *testing.T) {
	// A zero or unset execTimeout must not be forwarded to the sandbox backend;
	// the handler must reject the call with a clear configuration error.
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	b := &stubBackend{result: sandbox.RunResult{ExitCode: 0, Output: "ok"}}
	d.EnableExecution(b, []string{"go", "test", "./..."}, 0)
	_, err := d.Execute(execCtx(), "run_script", json.RawMessage(`{"content":"echo hi\n","timeout":5}`))
	require.Error(t, err, "run_script with a zero execTimeout must be a configuration error")
	var te *ToolError
	assert.ErrorAs(t, err, &te)
	assert.Empty(t, b.last.Script, "a zero execTimeout must not be dispatched to the backend")
}

func TestDispatcher_SetLimits_RaceWithCapResult(t *testing.T) {
	// Regression test for the race between SetLimits writing d.limits and
	// capResult reading d.limits without synchronization.
	d := NewDispatcher(stubResolver{root: "/snap"}, DefaultLimits())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			d.SetLimits(Limits{MaxResultBytes: 100 + i})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = d.capResult(ToolResult{Content: "x"})
		}
	}()
	wg.Wait()
}
