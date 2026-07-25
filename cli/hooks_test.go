package cli

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The decorator's own behaviour (fire-once, payload capture, failure paths,
// panic containment, concurrency) is tested in internal/hookobs, where it
// lives. This file covers what cli owns: the exported surface, the adapter
// between the two payload types, the production wiring, default-behaviour
// parity, and the structural guards on invocation coverage.

type recordingObserver struct {
	got []ModelInvocation
}

func (r *recordingObserver) OnModelInvocation(mi ModelInvocation) {
	r.got = append(r.got, mi)
}

func chatServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const okCompletion = `{"choices":[{"message":{"role":"assistant","content":"HIGH: a finding"},` +
	`"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`

// --- AC 01-01: the exported surface must be reachable from another module ---

// TestHooks_ExportedSurfaceHasNoInternalTypes enforces AC 01-01 structurally:
// atcr-enterprise is a separate module, so any internal/ type appearing in an
// exported signature makes the hook unusable no matter how well documented.
func TestHooks_ExportedSurfaceHasNoInternalTypes(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "hooks.go", nil, 0)
	require.NoError(t, err, "cli/hooks.go must exist and parse")

	internalPkgs := map[string]bool{
		"llmclient": true, "fanout": true, "telemetry": true, "log": true,
		"report": true, "circuitbreaker": true, "hookobs": true,
	}

	var offenders []string
	checkExpr := func(where string, e ast.Expr) {
		ast.Inspect(e, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if ident, ok := sel.X.(*ast.Ident); ok && internalPkgs[ident.Name] {
				offenders = append(offenders, fmt.Sprintf("%s: %s.%s", where, ident.Name, sel.Sel.Name))
			}
			return true
		})
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil || !d.Name.IsExported() {
				continue
			}
			checkExpr("func "+d.Name.Name, d.Type)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				checkExpr("type "+ts.Name.Name, ts.Type)
			}
		}
	}

	assert.Empty(t, offenders,
		"exported hook surface must reference no internal/ type — a separate module cannot import them")
}

// --- the adapter between the internal and exported payloads ----------------

// TestObserverAdapter_ConvertsEveryField guards the seam where the two payload
// types meet. A field added to one side and forgotten here is a silently empty
// column in the consumer's audit ledger, which no other test would catch.
func TestObserverAdapter_ConvertsEveryField(t *testing.T) {
	obs := &recordingObserver{}
	started := time.Now().UTC()

	temp := 0.2
	maxTok := 4096

	observerAdapter{observer: obs}.OnModelInvocation(hookobs.Invocation{
		Seq:       42,
		RunID:     "2026-07-25_feat-x",
		AgentName: "security-reviewer",
		Stage:     "verify",
		Model:     "test/model-a",
		Provider:  "openrouter",
		BaseURL:   "https://gateway.example/v1",
		Prompt:    "review this diff",
		Messages: []hookobs.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "", ToolCalls: []hookobs.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
			{Role: "tool", Content: "contents", ToolCallID: "c1"},
		},
		Response:          "HIGH: a finding",
		ResponseToolCalls: []hookobs.ToolCall{{ID: "c2", Name: "run_tests", Arguments: `{}`}},
		FinishReason:      "stop",
		Truncated:         true,
		PromptTokens:      11,
		CompletionTokens:  7,
		CostUSD:           0.000123,
		Temperature:       &temp,
		MaxTokens:         &maxTok,
		Attempts:          2,
		StartedAt:         started,
		Duration:          3 * time.Second,
		Err:               "boom",
	})

	require.Len(t, obs.got, 1)
	assert.Equal(t, ModelInvocation{
		Seq:       42,
		RunID:     "2026-07-25_feat-x",
		AgentName: "security-reviewer",
		Stage:     "verify",
		Model:     "test/model-a",
		Provider:  "openrouter",
		BaseURL:   "https://gateway.example/v1",
		Prompt:    "review this diff",
		Messages: []ModelMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "", ToolCalls: []ModelToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
			{Role: "tool", Content: "contents", ToolCallID: "c1"},
		},
		Response:          "HIGH: a finding",
		ResponseToolCalls: []ModelToolCall{{ID: "c2", Name: "run_tests", Arguments: `{}`}},
		FinishReason:      "stop",
		Truncated:         true,
		PromptTokens:      11,
		CompletionTokens:  7,
		CostUSD:           0.000123,
		Temperature:       &temp,
		MaxTokens:         &maxTok,
		Attempts:          2,
		StartedAt:         started,
		Duration:          3 * time.Second,
		Err:               "boom",
	}, obs.got[0])
}

// TestObserverAdapter_FieldCountsMatch is the guard that keeps the test above
// honest. That test only catches an unmapped field if someone remembers to add
// it to both literals; this one fails the moment the two structs drift, which
// is the actual risk — a field added to the internal payload and silently never
// surfaced to the consumer.
func TestObserverAdapter_FieldCountsMatch(t *testing.T) {
	assert.Equal(t,
		reflect.TypeOf(hookobs.Invocation{}).NumField(),
		reflect.TypeOf(ModelInvocation{}).NumField(),
		"hookobs.Invocation and cli.ModelInvocation have drifted — map the new field in observerAdapter")
	assert.Equal(t,
		reflect.TypeOf(hookobs.Message{}).NumField(),
		reflect.TypeOf(ModelMessage{}).NumField(),
		"hookobs.Message and cli.ModelMessage have drifted")
	assert.Equal(t,
		reflect.TypeOf(hookobs.ToolCall{}).NumField(),
		reflect.TypeOf(ModelToolCall{}).NumField(),
		"hookobs.ToolCall and cli.ModelToolCall have drifted")
}

// TestObserverAdapter_NoMessagesStaysNil keeps single-shot records free of an
// empty slice that would serialize as [] rather than being omitted.
func TestObserverAdapter_NoMessagesStaysNil(t *testing.T) {
	obs := &recordingObserver{}

	observerAdapter{observer: obs}.OnModelInvocation(hookobs.Invocation{Model: "m"})

	require.Len(t, obs.got, 1)
	assert.Nil(t, obs.got[0].Messages)
}

// --- production wiring -----------------------------------------------------

// TestNewCompleter_InstallsDecoratorWhenHookRegistered covers the function the
// production call sites actually call. Without it, rewriting newCompleter's
// body to `return llmclient.New()` — removing observation from every command in
// the binary — would leave the rest of the suite green.
func TestNewCompleter_InstallsDecoratorWhenHookRegistered(t *testing.T) {
	ctx := withHooks(context.Background(), Hooks{ModelInvocation: &recordingObserver{}}, &bytes.Buffer{})

	c := newCompleter(ctx)

	_, isBare := c.(*llmclient.Client)
	assert.False(t, isBare, "newCompleter must install the observing decorator when a hook is registered")
}

// TestNewCompleter_NoHookYieldsBareClient is the AC 01-03 half of the same
// guarantee, asserted at the production seam rather than on a helper.
func TestNewCompleter_NoHookYieldsBareClient(t *testing.T) {
	c := newCompleter(context.Background())

	_, isClient := c.(*llmclient.Client)
	assert.True(t, isClient, "the default path must hand the engine the same *llmclient.Client as before")
}

// TestNewCompleter_ZeroHooksYieldsBareClient covers MainWithHooks(..., Hooks{}).
func TestNewCompleter_ZeroHooksYieldsBareClient(t *testing.T) {
	ctx := withHooks(context.Background(), Hooks{}, &bytes.Buffer{})

	c := newCompleter(ctx)

	_, isClient := c.(*llmclient.Client)
	assert.True(t, isClient, "a zero Hooks must not install a wrapper")
}

// TestNewCompleter_SatisfiesCompleterFamily guards the subtlest failure mode:
// the engine picks its call path by type assertion (MetaCompleter first, then
// UsageCompleter, then plain Complete; ChatCompleter for tool-enabled agents).
// A decorator implementing only Complete would silently downgrade every agent
// to the no-usage, no-truncation path — the hook would still fire, but with
// zero token counts, making the audit record structurally wrong rather than
// visibly broken.
func TestNewCompleter_SatisfiesCompleterFamily(t *testing.T) {
	ctx := withHooks(context.Background(), Hooks{ModelInvocation: &recordingObserver{}}, &bytes.Buffer{})

	c := newCompleter(ctx)

	_, isUsage := c.(fanout.UsageCompleter)
	_, isMeta := c.(fanout.MetaCompleter)
	_, isChat := c.(fanout.ChatCompleter)
	assert.True(t, isUsage, "decorator must satisfy fanout.UsageCompleter")
	assert.True(t, isMeta, "decorator must satisfy fanout.MetaCompleter")
	assert.True(t, isChat, "decorator must satisfy fanout.ChatCompleter")
}

// TestNewCompleter_ObservesRealInvocation closes the loop end to end: a
// completer obtained the way production obtains it, driven against a real
// provider stub, must deliver a populated exported observation.
func TestNewCompleter_ObservesRealInvocation(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	t.Setenv("ATCR_HOOKS_TEST_KEY", "sk-test-not-a-real-key")
	obs := &recordingObserver{}
	ctx := withHooks(context.Background(), Hooks{ModelInvocation: obs}, &bytes.Buffer{})

	// Driven through CompleteWithMeta because that is the path the fan-out
	// engine actually selects (it type-asserts MetaCompleter first), and it is
	// the only single-shot path that carries token usage.
	mc, ok := newCompleter(ctx).(fanout.MetaCompleter)
	require.True(t, ok, "the production seam must expose the engine's preferred path")

	comp, err := mc.CompleteWithMeta(ctx, llmclient.Invocation{
		BaseURL:   srv.URL,
		APIKeyEnv: "ATCR_HOOKS_TEST_KEY",
		Model:     "test/model-a",
		Prompt:    "review this diff",
	})
	require.NoError(t, err)
	require.Equal(t, "HIGH: a finding", comp.Content)

	require.Len(t, obs.got, 1, "the production seam must observe the invocation exactly once")
	assert.Equal(t, "test/model-a", obs.got[0].Model)
	assert.Equal(t, "review this diff", obs.got[0].Prompt)
	assert.Equal(t, "HIGH: a finding", obs.got[0].Response)
	assert.Equal(t, 11, obs.got[0].PromptTokens)
}

// --- AC 01-03: default behaviour compatibility -----------------------------

// TestMainWithHooks_ZeroHooks_MatchesMain is AC 01-03's regression gate: with
// no hook registered the new entry point must be byte-for-byte
// indistinguishable from cli.Main on stdout, stderr, and exit code.
func TestMainWithHooks_ZeroHooks_MatchesMain(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "version", args: []string{"atcr", "--version"}},
		{name: "help", args: []string{"atcr", "--help"}},
		{name: "unknown subcommand", args: []string{"atcr", "definitely-not-a-command"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			t.Cleanup(func() { os.Args = origArgs })

			os.Args = tc.args
			var mainOut, mainErr bytes.Buffer
			mainCode := Main(context.Background(), &mainOut, &mainErr)

			os.Args = tc.args
			var hookOut, hookErr bytes.Buffer
			hookCode := MainWithHooks(context.Background(), &hookOut, &hookErr, Hooks{})

			assert.Equal(t, mainCode, hookCode, "exit code must be unchanged")
			assert.Equal(t, mainOut.String(), hookOut.String(), "stdout must be byte-for-byte unchanged")
			assert.Equal(t, mainErr.String(), hookErr.String(), "stderr must be byte-for-byte unchanged")
		})
	}
}

// --- structural coverage guards --------------------------------------------

// observedClientAllowlist names the non-test files permitted to call
// llmclient.New() directly. Everything else must route through
// hookobs.Wrap/newCompleter or its model calls are invisible to the audit
// trail.
//
//   - cli/hooks.go and internal/hookobs/hookobs.go: the seam itself.
//   - internal/verify/{pipeline,executor}.go and internal/debate/debate.go:
//     these call llmclient.New() *inside* hookobs.Wrap(...), so they are
//     observed. TestHooks_AllowlistedSitesActuallyWrap verifies that rather
//     than taking it on trust.
//   - cli/doctor.go: structurally out of scope. `atcr doctor` is a provider
//     self-test that hands its client to internal/doctor's own completer
//     interface and never enters the fan-out engine, so a doctor run produces
//     no observations. Documented on cli.Hooks.ModelInvocation.
var observedClientAllowlist = map[string]bool{
	"cli/hooks.go":                true,
	"internal/hookobs/hookobs.go": true,
	"internal/verify/pipeline.go": true,
	"internal/verify/executor.go": true,
	"internal/debate/debate.go":   true,
	"cli/doctor.go":               true,
}

// TestHooks_ModelClientsAreObserved walks the whole module's AST for
// llmclient.New() calls and requires each one to be either allowlisted above or
// lexically wrapped in hookobs.Wrap(...). This replaces an earlier substring
// scan over cli/*.go that could not see the real gap: verify, debate, and the
// fix executor each built their own unobserved client, so `atcr review
// --verify/--debate/--auto-fix` produced no audit records at all.
func TestHooks_ModelClientsAreObserved(t *testing.T) {
	root := ".."
	fset := token.NewFileSet()
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if name := info.Name(); name == ".git" || name == "testdata" || name == ".treehouse" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		if observedClientAllowlist[rel] {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // unparseable files are not this test's concern
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "New" {
				return true
			}
			if ident, ok := sel.X.(*ast.Ident); !ok || ident.Name != "llmclient" {
				return true
			}
			offenders = append(offenders, fmt.Sprintf("%s:%d", rel, fset.Position(call.Pos()).Line))
			return true
		})
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		"these sites build an unobserved model client; wrap them in hookobs.Wrap(ctx, ...) "+
			"or add them to observedClientAllowlist with a reason")
}

// TestHooks_AllowlistedSitesActuallyWrap keeps the allowlist honest for the
// three packages listed as "wrapped, not exempt": if verify or debate ever
// stops wrapping, the allowlist would otherwise hide the regression.
func TestHooks_AllowlistedSitesActuallyWrap(t *testing.T) {
	for _, rel := range []string{
		"internal/verify/pipeline.go",
		"internal/verify/executor.go",
		"internal/debate/debate.go",
	} {
		src, err := os.ReadFile(filepath.Join("..", rel))
		require.NoError(t, err, "allowlist names a file that no longer exists: %s", rel)
		assert.Contains(t, string(src), "hookobs.Wrap(",
			"%s is allowlisted on the grounds that it wraps its client — it no longer does", rel)
	}
}

// TestHooks_DoctorExemptionIsStillAccurate asserts the assumption the doctor
// exemption rests on: internal/doctor does not route through internal/fanout,
// so this seam structurally cannot observe it. An earlier version of this test
// read cli/doctor.go, which is not where that assumption lives and would have
// stayed green through exactly the regression it claimed to catch.
func TestHooks_DoctorExemptionIsStillAccurate(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join("..", "internal", "doctor", "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "internal/doctor must exist for its exemption to mean anything")

	fset := token.NewFileSet()
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		require.NoError(t, perr)
		for _, imp := range file.Imports {
			assert.NotEqual(t, `"github.com/samestrin/atcr/internal/fanout"`, imp.Path.Value,
				"%s now imports fanout — revisit the doctor coverage exemption in cli/hooks.go", path)
		}
	}
}
