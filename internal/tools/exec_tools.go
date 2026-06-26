package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/sandbox"
)

// ExecutionTools returns the execution-only tool definitions (Epic 11.0). They
// are NOT part of Tools(): they are offered solely to execution-enabled agents
// (see fanout.wireToolDefs) and only execute when a dispatcher has an exec
// backend wired via EnableExecution. Keeping them out of the default set
// preserves the read-only guarantee of the v1 tool harness for every other agent.
func ExecutionTools() []ToolDef {
	return []ToolDef{runTestsDef(), runScriptDef()}
}

func runTestsDef() ToolDef {
	return ToolDef{
		Name: "run_tests",
		Description: "Run the project's test suite inside the sandbox (network-isolated, read-only snapshot) " +
			"and return the exit code and output. Optional target scopes the run (e.g. a package path). " +
			"Use this to PROVE a finding by reproducing a failure.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{"type": "string", "description": "Optional package/path to scope the test run."},
			},
			"required": []string{},
		},
	}
}

func runScriptDef() ToolDef {
	return ToolDef{
		Name: "run_script",
		Description: "Run a short shell script inside the sandbox (network-isolated, read-only snapshot, writable " +
			"/scratch) and return the exit code and output. Use this to write a minimal reproduction of a bug.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string", "description": "The shell script body to execute with /bin/sh."},
				"timeout": map[string]any{"type": "integer", "description": "Optional per-run timeout in seconds."},
			},
			"required": []string{"content"},
		},
	}
}

// execEligibilityKey is the context key under which a caller's exec eligibility
// is carried. Its unexported type prevents collision with any other package's
// context values.
type execEligibilityKey struct{}

// WithExecEligibility returns a context that grants (allowed=true) or denies the
// caller permission to invoke execution-gated tools (run_tests/run_script).
// Dispatcher.Execute reads this per call: it is the structural, per-caller gate
// that makes the read-only boundary independent of how a dispatcher was wired.
// Eligibility is FAIL-CLOSED — a context without this value (or with false)
// cannot reach an exec tool, so a caller that does not affirmatively grant
// eligibility can never escalate to execution.
func WithExecEligibility(ctx context.Context, allowed bool) context.Context {
	return context.WithValue(ctx, execEligibilityKey{}, allowed)
}

// execEligible reports whether ctx was granted exec eligibility. Absent value =
// not eligible (default-deny).
func execEligible(ctx context.Context) bool {
	allowed, _ := ctx.Value(execEligibilityKey{}).(bool)
	return allowed
}

// EnableExecution wires a sandbox backend into the dispatcher and registers the
// run_tests/run_script tools. testCmd is the project's test command (from
// config); timeout is the default per-run budget. It is called once, during
// construction, only when the operator opted into `--exec` with a backend that
// passed Preflight — so the dispatcher's default (no-exec) build keeps exactly
// the three read-only tools.
func (d *Dispatcher) EnableExecution(backend sandbox.Backend, testCmd []string, timeout time.Duration) {
	d.execBackend = backend
	d.execTestCmd = append([]string(nil), testCmd...)
	d.execTimeout = timeout
	// registerExec is the single trusted exec-registration path: it atomically
	// co-sets each handler with its execTools gate under one lock, so a concurrent
	// Execute can never observe an exec handler without its gate (no fail-OPEN
	// window). It also bypasses the public RegisterTool name guard by design —
	// run_tests/run_script match execToolPatterns, and keeping those names out of
	// the public API is exactly that guard's purpose.
	d.registerExec("run_tests", runTestsHandler)
	d.registerExec("run_script", runScriptHandler)
}

type runTestsArgs struct {
	Target string `json:"target"`
}

func runTestsHandler(ctx context.Context, d *Dispatcher, argsJSON json.RawMessage, _ string) (ToolResult, error) {
	var a runTestsArgs
	if err := unmarshalToolArgs(argsJSON, &a); err != nil {
		return ToolResult{}, toolErrf("run_tests: invalid arguments: %v", err)
	}
	cmd := append([]string(nil), d.execTestCmd...)
	if t := strings.TrimSpace(a.Target); t != "" {
		// Reject a flag-like target: appended verbatim to the test command, a value
		// beginning with '-' would be parsed as an option (argument injection) rather
		// than the package/path scope the parameter is documented to be.
		if strings.HasPrefix(t, "-") {
			return ToolResult{}, toolErrf("run_tests: target %q must not start with '-'", t)
		}
		cmd = append(cmd, t)
	}
	return d.runInSandbox(ctx, sandbox.RunSpec{Command: cmd, SnapshotDir: d.root, Timeout: d.execTimeout})
}

// maxScriptBytes caps the size of a model-supplied run_script body. It mirrors
// the sandbox's default MaxOutputBytes (64 KiB): a reproduction script has no
// legitimate need to be larger, and bounding it before dispatch keeps an
// oversized body from being buffered host-side.
const maxScriptBytes = 64 * 1024

type runScriptArgs struct {
	Content string `json:"content"`
	Timeout int    `json:"timeout"`
}

func runScriptHandler(ctx context.Context, d *Dispatcher, argsJSON json.RawMessage, _ string) (ToolResult, error) {
	var a runScriptArgs
	if err := unmarshalToolArgs(argsJSON, &a); err != nil {
		return ToolResult{}, toolErrf("run_script: invalid arguments: %v", err)
	}
	if strings.TrimSpace(a.Content) == "" {
		return ToolResult{}, toolErrf("run_script: content is required")
	}
	// Bound the script size before it ever reaches the sandbox: an unbounded
	// model-supplied body is buffered host-side and could exhaust host memory.
	if len(a.Content) > maxScriptBytes {
		return ToolResult{}, toolErrf("run_script: content too large (%d bytes, max %d)", len(a.Content), maxScriptBytes)
	}
	// Clamp a model-supplied per-call timeout to the operator's configured
	// per-run budget: the override may only SHORTEN a run, never extend it past
	// d.execTimeout (which SandboxConfig.Validate already bounded by
	// MaxTimeoutSecs). Non-positive values keep the default budget.
	timeout := d.execTimeout
	if a.Timeout > 0 {
		if req := time.Duration(a.Timeout) * time.Second; req < timeout {
			timeout = req
		}
	}
	return d.runInSandbox(ctx, sandbox.RunSpec{Script: a.Content, SnapshotDir: d.root, Timeout: timeout})
}

// runInSandbox executes spec on the wired backend and renders the result into a
// model-facing tool message: the command, the exit code (with a timeout marker),
// then the captured output. A nil backend is a programming error (handlers are
// only registered by EnableExecution), surfaced as a tool error rather than a panic.
func (d *Dispatcher) runInSandbox(ctx context.Context, spec sandbox.RunSpec) (ToolResult, error) {
	if d.execBackend == nil {
		return ToolResult{}, toolErrf("execution backend not configured")
	}
	res, err := d.execBackend.Run(ctx, spec)
	if err != nil {
		return ToolResult{}, toolErrf("sandbox run failed: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "$ %s\n", res.Command)
	if res.TimedOut {
		fmt.Fprintf(&b, "exit code: %d (timed out)\n", res.ExitCode)
	} else {
		fmt.Fprintf(&b, "exit code: %d\n", res.ExitCode)
	}
	b.WriteString(res.Output)
	return ToolResult{Content: b.String()}, nil
}

// unmarshalToolArgs unmarshals tool arguments, tolerating empty/whitespace input
// as an empty object so optional-only tools accept a bare call.
func unmarshalToolArgs(argsJSON json.RawMessage, v any) error {
	trimmed := strings.TrimSpace(string(argsJSON))
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	return json.Unmarshal([]byte(trimmed), v)
}
