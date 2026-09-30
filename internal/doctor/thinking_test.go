package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const thinkingKey = "sk-thinking-secret"

// thinkingTarget resolves one agent with the given thinking declaration.
func thinkingTarget(t *testing.T, thinking, level, style string) *Resolution {
	t.Helper()
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m",
			Thinking: thinking, ThinkingLevel: level, ThinkingStyle: style,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	return res
}

// withMarker is a Completion carrying the marker, so the endpoint probe is ok.
func withMarker(c llmclient.Completion) llmclient.Completion {
	c.Content = Marker(testNonce)
	return c
}

// silent is a marker reply with no reasoning signal and no reasoning-token field.
var silent = withMarker(llmclient.Completion{})

// thinks is a marker reply carrying reported reasoning tokens.
var thinks = withMarker(llmclient.Completion{Usage: llmclient.UsageData{ReasoningTokens: 32, ReasoningTokensReported: true}})

// runThinking runs doctor for one declared agent. The first CompleteWithMeta call
// (the declared marker probe) returns declared; any later call (the control)
// returns control.
func runThinking(t *testing.T, res *Resolution, declared llmclient.Completion, declaredErr error, control llmclient.Completion, controlErr error) (AgentResult, *fakeCompleter, *Report) {
	t.Helper()
	t.Setenv(rfDoctorEnvK, thinkingKey)
	fake := newFake(markerOK)
	n := 0
	fake.metaFn = func(llmclient.Invocation) (llmclient.Completion, error) {
		n++
		if n == 1 {
			return declared, declaredErr
		}
		return control, controlErr
	}
	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	require.Len(t, rep.Agents, 1)
	return rep.Agents[0], fake, rep
}

// AC 05-01 Scenario 3: the marker probe carries the target's own declaration, so
// the verdict measures the declared call, not the provider default.
func TestRun_ThinkingProbeSendsTheDeclaration(t *testing.T) {
	_, fake, _ := runThinking(t, thinkingTarget(t, registry.ThinkingOff, "", registry.ThinkingStyleQwen), thinks, nil, llmclient.Completion{}, nil)
	calls := fake.completeCalls()
	require.NotEmpty(t, calls)
	assert.Equal(t, registry.ThinkingOff, calls[0].Thinking)
	assert.Empty(t, calls[0].ThinkingLevel)
	assert.Equal(t, registry.ThinkingStyleQwen, calls[0].ThinkingStyle)
	assert.Equal(t, Prompt(testNonce), calls[0].Prompt)
}

// The response_format probe is the second doctor site: a declared agent's JSON-mode
// call carries its thinking declaration too.
func TestRun_ResponseFormatProbeSendsTheThinkingDeclaration(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m", ResponseFormat: registry.ResponseFormatJSONObject,
			ThinkingLevel: registry.ThinkingLevelLow, ThinkingStyle: registry.ThinkingStyleReasoningEffort,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	t.Setenv(rfDoctorEnvK, thinkingKey)
	fake := newFake(markerOK)
	fake.metaFn = func(llmclient.Invocation) (llmclient.Completion, error) { return thinks, nil }
	fake.chatFn = reply(oneFinding)
	Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	chats := fake.chatCalls()
	require.Len(t, chats, 1)
	assert.Empty(t, chats[0].inv.Thinking)
	assert.Equal(t, registry.ThinkingLevelLow, chats[0].inv.ThinkingLevel)
	assert.Equal(t, registry.ThinkingStyleReasoningEffort, chats[0].inv.ThinkingStyle)
}

// AC 05-02: the four documented verdict cases, plus the control call, truncation,
// and the thinking: on polarity.
func TestRun_ThinkingVerdict(t *testing.T) {
	reasoningOnly := withMarker(llmclient.Completion{Reasoning: "let me think"})
	reportedZero := withMarker(llmclient.Completion{Usage: llmclient.UsageData{ReasoningTokensReported: true}})
	truncatedSilent := withMarker(llmclient.Completion{Truncated: true})
	truncatedThinks := withMarker(llmclient.Completion{Truncated: true, Usage: llmclient.UsageData{ReasoningTokens: 900, ReasoningTokensReported: true}})
	truncatedReportedZero := withMarker(llmclient.Completion{Truncated: true, Usage: llmclient.UsageData{ReasoningTokensReported: true}})
	// A Qwen-family upstream can put its thinking inline in the content instead
	// of on a reasoning channel, and a proxy can still report 0 reasoning tokens.
	inlineThink := llmclient.Completion{Content: "<think>plan the reply</think>\n" + Marker(testNonce)}
	withEmptyThink := llmclient.Completion{Content: "<think>\n\n</think>\n" + Marker(testNonce)}
	inlineThinkReportedZero := inlineThink
	inlineThinkReportedZero.Usage = llmclient.UsageData{ReasoningTokensReported: true}

	cases := []struct {
		name                  string
		thinking, level       string
		style                 string
		declared              llmclient.Completion
		declaredErr           error
		control               llmclient.Completion
		controlErr            error
		wantStatus            string
		wantCalls             int
		wantDetail, notDetail []string
	}{
		{name: "off, reasoning tokens present", thinking: "off", style: "qwen", declared: thinks,
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"32 reasoning tokens", "thinking: off"}},
		{name: "off, reasoning content alone", thinking: "off", style: "qwen", declared: reasoningOnly,
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"reasoning content"}},
		{name: "off, provider reports zero", thinking: "off", style: "qwen", declared: reportedZero,
			wantStatus: ThinkingHonored, wantCalls: 1},
		{name: "off, silent, control thinks", thinking: "off", style: "template_kwargs", declared: silent, control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2, wantDetail: []string{"without the declaration", "32 reasoning tokens"}},
		{name: "off, silent, control reports zero", thinking: "off", style: "qwen", declared: silent, control: reportedZero,
			wantStatus: ThinkingHonored, wantCalls: 2},
		{name: "off, silent on both", thinking: "off", style: "qwen", declared: silent, control: silent,
			wantStatus: ThinkingUnverified, wantCalls: 2, wantDetail: []string{"no reasoning signal"}},
		{name: "off, silent, control 429", thinking: "off", style: "qwen", declared: silent, controlErr: &llmclient.HTTPStatusError{Status: 429, Snippet: "slow down"},
			wantStatus: ThinkingUnverified, wantCalls: 2, wantDetail: []string{"control", "HTTP 429"}},
		{name: "off, silent, control truncated", thinking: "off", style: "qwen", declared: silent, control: truncatedSilent,
			wantStatus: ThinkingUnverified, wantCalls: 2},
		{name: "declared call 429", thinking: "off", style: "qwen", declaredErr: &llmclient.HTTPStatusError{Status: 429, Snippet: "slow down"},
			wantStatus: ThinkingUnverified, wantCalls: 1, wantDetail: []string{"the thinking probe got HTTP 429, so no verdict was reached: slow down"}},
		{name: "declared call 503", thinking: "off", style: "qwen", declaredErr: &llmclient.HTTPStatusError{Status: 503, Snippet: "down"},
			wantStatus: ThinkingUnverified, wantCalls: 1, wantDetail: []string{"HTTP 503"}},
		// A failed endpoint whose failure a re-run cannot fix (auth, model name,
		// transport) gets NO thinking verdict, mirroring the response_format gate:
		// against a failed one the probe "can only fail the same way and bury the
		// real cause". Only transient classes (429, 5xx, timeout) keep unverified.
		{name: "declared call 401 auth", thinking: "off", style: "qwen", declaredErr: &llmclient.HTTPStatusError{Status: 401, Snippet: "bad key"},
			wantStatus: "", wantCalls: 1},
		{name: "declared call 404 model", thinking: "off", style: "qwen", declaredErr: &llmclient.HTTPStatusError{Status: 404, Snippet: "no model"},
			wantStatus: "", wantCalls: 1},
		{name: "declared call transport error", thinking: "off", style: "qwen", declaredErr: errors.New("connection reset"),
			wantStatus: "", wantCalls: 1},
		{name: "declared call timeout", thinking: "off", style: "qwen", declaredErr: context.DeadlineExceeded,
			wantStatus: ThinkingUnverified, wantCalls: 1},
		{name: "declared call empty reply", thinking: "off", style: "qwen", declaredErr: errors.New("provider returned an empty completion"),
			wantStatus: "", wantCalls: 1},
		{name: "truncated, no signal", thinking: "off", style: "qwen", declared: truncatedSilent,
			wantStatus: ThinkingUnverified, wantCalls: 1, wantDetail: []string{"cut off at the output cap (2048 tokens)", "max_tokens"}},
		{name: "truncated, signal wins", thinking: "off", style: "qwen", declared: truncatedThinks,
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"900 reasoning tokens", "cut off", "max_tokens"}},
		{name: "on, signal present", thinking: "on", style: "qwen", declared: thinks,
			wantStatus: ThinkingHonored, wantCalls: 1},
		{name: "level, signal present, level not claimed", level: "low", style: "reasoning_effort", declared: thinks,
			wantStatus: ThinkingHonored, wantCalls: 1, wantDetail: []string{"does not verify thinking_level low"}},
		{name: "on, provider reports zero", thinking: "on", style: "qwen", declared: reportedZero,
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"thinking: on"}},
		{name: "on, silent, control thinks", thinking: "on", style: "qwen", declared: silent, control: thinks,
			wantStatus: ThinkingNotHonored, wantCalls: 2},
		// Phase 4 gate decision (option A): a level legitimately lowers reasoning, and
		// a short prompt can need none, so silence under a level is not a failure.
		// Reasoning on the control call shows the declaration changed the reply.
		{name: "level, silent, control thinks", level: "low", style: "reasoning_effort", declared: silent, control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2, wantDetail: []string{"changed the reply", "does not verify thinking_level low"}},
		{name: "level, reported zero still sends the control", level: "low", style: "reasoning_effort", declared: reportedZero, control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2},
		{name: "level, silent, control reports zero", level: "low", style: "reasoning_effort", declared: silent, control: reportedZero,
			wantStatus: ThinkingUnverified, wantCalls: 2},
		{name: "on with level, silent, control thinks", thinking: "on", level: "high", style: "qwen", declared: silent, control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2},
		{name: "off, inline think tags", thinking: "off", style: "qwen", declared: inlineThink,
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"inline <think> reasoning in the content"}},
		{name: "off, reported zero but inline think tags", thinking: "off", style: "qwen", declared: inlineThinkReportedZero,
			wantStatus: ThinkingNotHonored, wantCalls: 1},
		// An empty think pair or blank reasoning is what a hybrid template emits
		// when thinking is correctly off; it is not a signal.
		{name: "off, empty think pair", thinking: "off", style: "qwen", declared: withEmptyThink, control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2, notDetail: []string{"inline <think> reasoning in the content", "cut off"}},
		{name: "off, blank reasoning", thinking: "off", style: "template_kwargs", declared: withMarker(llmclient.Completion{Reasoning: " \n "}), control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2, notDetail: []string{"reasoning content", "inline <think> reasoning in the content", "cut off"}},
		{name: "off, unclosed think with text", thinking: "off", style: "qwen", declared: llmclient.Completion{Content: "<think>still going"},
			wantStatus: ThinkingNotHonored, wantCalls: 1},
		// An opener followed only by whitespace carries no reasoning: no signal.
		{name: "off, unclosed think with blank remainder", thinking: "off", style: "qwen", declared: llmclient.Completion{Content: "<think>   "}, control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2, notDetail: []string{"inline <think> reasoning in the content"}},
		// A closing tag with no opener anywhere IS a signal: a reasoning-style chat
		// template can put the opener in the prompt, so the reply starts mid-thought
		// and carries only the closer, and the text before it is reasoning.
		// (classify reads the raw content for the marker, so this detection never
		// touches marker validation.)
		{name: "off, closing tag only", thinking: "off", style: "qwen", declared: llmclient.Completion{Content: "planning the reply</think>\n" + Marker(testNonce)},
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"inline <think> reasoning in the content"}},
		// Detection is position-blind, unlike the review lanes' leading-only strip:
		// this prompt contains no tag, so a block after the answer is not the model
		// quoting one. Phase 1 review decision, 2026-09-30.
		{name: "off, think pair after answer text", thinking: "off", style: "qwen", declared: llmclient.Completion{Content: "answer <think>x</think> more\n" + Marker(testNonce)},
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"inline <think> reasoning in the content"}},
		// The runaway thinker probeThinking exists to name: the model answers, then
		// keeps thinking until the cap cuts it off. A leading-only rule would miss it.
		{name: "off, unclosed think block after the answer", thinking: "off", style: "qwen", declared: llmclient.Completion{Truncated: true, Content: Marker(testNonce) + "\n<think>let me double check"},
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"inline <think> reasoning in the content"}},
		// An empty first pair must not hide a real think block after it.
		{name: "off, empty pair then real think block", thinking: "off", style: "qwen", declared: llmclient.Completion{Content: "<think></think><think>real reasoning</think>answer"},
			wantStatus: ThinkingNotHonored, wantCalls: 1, wantDetail: []string{"inline <think> reasoning in the content"}},
		// The sprint's flip gave up this direction, so pin it: an ANSWER that mentions a
		// tag the detector must NOT count stays out of the reasoning signal. Here the
		// answer names a VARIANT closer (</thinking>, the spelling SplitThink's doc calls
		// out), which is a tag to neither helper, so the reply is judged on its
		// reasoning channels alone and the control call carries the verdict.
		{name: "off, answer mentions a variant closer", thinking: "off", style: "qwen",
			declared: llmclient.Completion{Content: "I never look for </thinking> markers\n" + Marker(testNonce)}, control: thinks,
			wantStatus: ThinkingHonored, wantCalls: 2, notDetail: []string{"inline <think> reasoning in the content"}},
		// The control-call skip under `thinking: on` is deliberate: a lone closer IS a
		// signal to the position-blind detector, and a signal on the declared call
		// decides alone, so no control call is placed.
		{name: "on, closing tag only, signal decides alone", thinking: "on", style: "qwen",
			declared:   llmclient.Completion{Content: "planning the reply</think>\n" + Marker(testNonce)},
			wantStatus: ThinkingHonored, wantCalls: 1},
		// An empty reply cut off at the cap is the runaway thinker the verdict exists
		// to name: it classifies as network_error, yet must still get a verdict that
		// carries the cut-off remedy (TD internal/doctor/run.go:994).
		{name: "declared call empty and cut off", thinking: "off", style: "qwen", declared: llmclient.Completion{Truncated: true}, declaredErr: errors.New("provider returned an empty completion"),
			wantStatus: ThinkingUnverified, wantCalls: 1, wantDetail: []string{"the reply was cut off at the output cap (2048 tokens)", "max_tokens"}},
		{name: "off, silent, control cut off reporting zero", thinking: "off", style: "qwen", declared: silent, control: truncatedReportedZero,
			wantStatus: ThinkingUnverified, wantCalls: 2, wantDetail: []string{"control", "cut off"}},
		{name: "on, truncated, signal", thinking: "on", style: "qwen", declared: truncatedThinks,
			wantStatus: ThinkingHonored, wantCalls: 1, wantDetail: []string{"the reply was also cut off"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, fake, rep := runThinking(t, thinkingTarget(t, tc.thinking, tc.level, tc.style), tc.declared, tc.declaredErr, tc.control, tc.controlErr)
			assert.Equal(t, tc.wantStatus, a.ThinkingStatus, "detail: %s", a.ThinkingDetail)
			assert.Len(t, fake.completeCalls(), tc.wantCalls)
			for _, want := range tc.wantDetail {
				assert.Contains(t, a.ThinkingDetail, want)
			}
			for _, not := range tc.notDetail {
				assert.NotContains(t, a.ThinkingDetail, not)
			}
			if a.Status == StatusOK {
				assert.Equal(t, 0, rep.ExitCode, "a thinking verdict never changes the exit code")
			}
		})
	}
}

// Q1 decision: the control call is the declared call minus the declaration — same
// prompt, same cap, no thinking field.
// deadlineCompleter records whether the context it received carried a deadline,
// so a test can pin which calls the run's --timeout actually bounds.
type deadlineCompleter struct {
	hasDeadline bool
	remaining   time.Duration
}

func (f *deadlineCompleter) CompleteWithMeta(ctx context.Context, inv llmclient.Invocation) (llmclient.Completion, error) {
	if dl, ok := ctx.Deadline(); ok {
		f.hasDeadline = true
		f.remaining = time.Until(dl)
	}
	return llmclient.Completion{Content: Marker(testNonce)}, nil
}

func (f *deadlineCompleter) Chat(ctx context.Context, inv llmclient.Invocation, messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	return nil, errors.New("not used")
}

// TD-019: the marker, response_format, and thinking control calls each apply
// --timeout independently, so one slow target can hold its concurrency slot for
// several timeouts. The bound is accepted and documented beside the control call
// (run.go, thinkingControlCall) rather than restructured into one shared deadline;
// this test pins that the control call really does carry the run deadline, that a
// parent deadline survives, and that none is manufactured when no timeout is set.
func TestThinkingControlCallCarriesTheRunDeadline(t *testing.T) {
	t.Setenv(rfDoctorEnvK, thinkingKey)
	tgt := Target{BaseURL: "https://api.example/v1", APIKeyEnv: rfDoctorEnvK, Model: "m"}

	capped := &deadlineCompleter{}
	_, err := thinkingControlCall(context.Background(), capped, tgt, Options{Nonce: testNonce, Timeout: 2 * time.Second}, 0)
	require.NoError(t, err)
	assert.True(t, capped.hasDeadline, "control call must carry the --timeout deadline")
	assert.Greater(t, capped.remaining, time.Duration(0))
	assert.LessOrEqual(t, capped.remaining, 2*time.Second)

	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inherited := &deadlineCompleter{}
	_, err = thinkingControlCall(parent, inherited, tgt, Options{Nonce: testNonce}, 0)
	require.NoError(t, err)
	assert.True(t, inherited.hasDeadline, "a parent deadline must reach the control call")

	uncapped := &deadlineCompleter{}
	_, err = thinkingControlCall(context.Background(), uncapped, tgt, Options{Nonce: testNonce}, 0)
	require.NoError(t, err)
	assert.False(t, uncapped.hasDeadline, "no deadline may be manufactured without --timeout")
}

func TestRun_ThinkingControlCallDropsOnlyTheDeclaration(t *testing.T) {
	_, fake, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), silent, nil, thinks, nil)
	calls := fake.completeCalls()
	require.Len(t, calls, 2)
	declared, control := calls[0], calls[1]
	assert.Empty(t, control.Thinking)
	assert.Empty(t, control.ThinkingLevel)
	assert.Empty(t, control.ThinkingStyle)
	assert.Equal(t, declared.Prompt, control.Prompt)
	require.NotNil(t, control.MaxTokens)
	assert.Equal(t, *declared.MaxTokens, *control.MaxTokens)
	assert.Equal(t, declared.Model, control.Model)
	assert.Equal(t, declared.BaseURL, control.BaseURL)
}

// AC 05-02 Security: a provider error snippet is scrubbed of the API key before it
// is stored.
func TestRun_ThinkingDetailIsScrubbed(t *testing.T) {
	a, _, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), llmclient.Completion{}, &llmclient.HTTPStatusError{Status: 500, Snippet: "bad key " + thinkingKey}, llmclient.Completion{}, nil)
	assert.Equal(t, ThinkingUnverified, a.ThinkingStatus)
	assert.NotContains(t, a.ThinkingDetail, thinkingKey)
	assert.Contains(t, a.ThinkingDetail, "[redacted]")
}

// Every error detail is scrubbed: the transport branch and the control call too,
// and the key is removed before the detail is bounded, so a key straddling the
// bound cannot leave a prefix behind.
func TestRun_ThinkingDetailIsScrubbedOnEveryErrorPath(t *testing.T) {
	transport := errors.New("dial failed for " + thinkingKey)
	a, _, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), llmclient.Completion{}, transport, llmclient.Completion{}, nil)
	assert.NotContains(t, a.ThinkingDetail, thinkingKey)

	a, _, _ = runThinking(t, thinkingTarget(t, "off", "", "qwen"), silent, nil, llmclient.Completion{}, transport)
	assert.Equal(t, ThinkingUnverified, a.ThinkingStatus)
	assert.NotContains(t, a.ThinkingDetail, thinkingKey)

	straddle := errors.New(strings.Repeat("x", maxDetailBytes-7) + thinkingKey)
	a, _, _ = runThinking(t, thinkingTarget(t, "off", "", "qwen"), llmclient.Completion{}, straddle, llmclient.Completion{}, nil)
	assert.NotContains(t, a.ThinkingDetail, thinkingKey[:7])
}

// AC 05-01 Scenario 2 and the story's fifth case: an undeclared agent gets no
// verdict, no extra call, and no thinking field on its probe.
func TestRun_UndeclaredAgentGetsNoThinkingProbe(t *testing.T) {
	a, fake, _ := runThinking(t, thinkingTarget(t, "", "", ""), thinks, nil, thinks, nil)
	assert.Empty(t, a.ThinkingStatus)
	assert.Empty(t, a.ThinkingDetail)
	calls := fake.completeCalls()
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].Thinking)
	assert.Empty(t, calls[0].ThinkingStyle)
}

// AC 05-01 Edge Case 1: a style-only declaration is inert, so it gets no probe.
func TestRun_StyleOnlyDeclarationGetsNoThinkingProbe(t *testing.T) {
	a, fake, _ := runThinking(t, thinkingTarget(t, "", "", "qwen"), thinks, nil, thinks, nil)
	assert.Empty(t, a.ThinkingStatus)
	assert.Len(t, fake.completeCalls(), 1)
}

// No call placed (missing key) means no thinking verdict; the row already fails.
func TestRun_NoThinkingVerdictWithoutACall(t *testing.T) {
	fake := newFake(markerOK)
	rep := Run(context.Background(), fake, thinkingTarget(t, "off", "", "qwen"), Options{Nonce: testNonce, MaxTokens: 2048})
	require.Len(t, rep.Agents, 1)
	assert.Equal(t, StatusMissingKey, rep.Agents[0].Status)
	assert.Empty(t, rep.Agents[0].ThinkingStatus)
	assert.Empty(t, rep.Agents[0].ThinkingPolarity, "thinking_declared is omitted exactly when thinking_status is")
	assert.Empty(t, fake.completeCalls())

	// A permanent failure places a call but still reaches no verdict.
	a, _, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), llmclient.Completion{}, &llmclient.HTTPStatusError{Status: 401, Snippet: "bad key"}, llmclient.Completion{}, nil)
	assert.Empty(t, a.ThinkingStatus)
	assert.Empty(t, a.ThinkingPolarity, "thinking_declared is omitted exactly when thinking_status is")
}

// AC 05-04 Error Scenario 2: not_honored alone never changes the status, the
// ok/failed count, or the exit code.
func TestRun_ThinkingNotHonoredIsNonBlocking(t *testing.T) {
	a, _, rep := runThinking(t, thinkingTarget(t, "off", "", "qwen"), thinks, nil, llmclient.Completion{}, nil)
	assert.Equal(t, ThinkingNotHonored, a.ThinkingStatus)
	assert.Equal(t, StatusOK, a.Status)
	assert.Empty(t, a.Hint)
	assert.Equal(t, 0, rep.ExitCode)
}

// A marker-absent row still answered, so it still gets a verdict.
func TestRun_ThinkingVerdictOnAnOkWarningRow(t *testing.T) {
	declared := llmclient.Completion{Content: "no marker here", Usage: llmclient.UsageData{ReasoningTokens: 5, ReasoningTokensReported: true}}
	a, _, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), declared, nil, llmclient.Completion{}, nil)
	assert.Equal(t, StatusOKWarning, a.Status)
	assert.Equal(t, ThinkingNotHonored, a.ThinkingStatus)
}

// AC 05-03: the dedup key grows only for a declared agent.
func TestResolve_ThinkingDeclarationJoinsTheTargetKey(t *testing.T) {
	off := registry.AgentConfig{Thinking: "off", ThinkingStyle: "qwen"}
	cases := []struct {
		name        string
		a, b        registry.AgentConfig
		wantTargets int
	}{
		{"declared and undeclared split", off, registry.AgentConfig{}, 2},
		{"identical declarations share", off, off, 1},
		{"different level splits", registry.AgentConfig{ThinkingLevel: "low", ThinkingStyle: "reasoning_effort"}, registry.AgentConfig{ThinkingLevel: "high", ThinkingStyle: "reasoning_effort"}, 2},
		{"different style splits", off, registry.AgentConfig{Thinking: "off", ThinkingStyle: "template_kwargs"}, 2},
		{"on and off split", off, registry.AgentConfig{Thinking: "on", ThinkingStyle: "qwen"}, 2},
		{"undeclared pair shares", registry.AgentConfig{}, registry.AgentConfig{}, 1},
		{"style-only is inert and shares", registry.AgentConfig{ThinkingStyle: "qwen"}, registry.AgentConfig{}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := declaredRegistry(t, map[string]registry.AgentConfig{"a": tc.a, "b": tc.b})
			assert.Len(t, res.Targets, tc.wantTargets)
		})
	}

	res := declaredRegistry(t, map[string]registry.AgentConfig{"a": off, "b": {}})
	ta, tb := targetForAgent(t, res, "a"), targetForAgent(t, res, "b")
	assert.Equal(t, "off", ta.Thinking)
	assert.Equal(t, "qwen", ta.ThinkingStyle)
	assert.Empty(t, tb.Thinking)
	assert.Empty(t, tb.ThinkingStyle)
	assert.Equal(t, []string{"a"}, res.Paths["a"])
	assert.Equal(t, []string{"b"}, res.Paths["b"])
}

// A style-only target carries no thinking fields, so its probe matches an
// undeclared agent's byte for byte.
func TestResolve_StyleOnlyTargetCarriesNoThinking(t *testing.T) {
	res := declaredRegistry(t, map[string]registry.AgentConfig{"a": {ThinkingStyle: "qwen"}})
	assert.Equal(t, Target{Provider: "p", Model: "same-model", BaseURL: "http://one-endpoint", APIKeyEnv: "K"}, res.Targets[0])
}

// AC 05-01 Scenario 2: an undeclared agent's JSON carries no thinking keys; a
// declared agent's carries the verdict.
func TestRenderJSON_ThinkingKeys(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{{Agent: "a", Status: StatusOK}}}))
	assert.NotContains(t, buf.String(), "thinking")

	buf.Reset()
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{{Agent: "a", Status: StatusOK, ThinkingStatus: ThinkingNotHonored, ThinkingDetail: "why"}}}))
	var out struct {
		Agents []map[string]any `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, ThinkingNotHonored, out.Agents[0]["thinking_status"])
	assert.Equal(t, "why", out.Agents[0]["thinking_detail"])
}

// AC 05-04: the HINT cell labels the thinking verdict after any response_format
// label, clamps a long detail, and adds nothing for honored.
func TestRenderTable_ThinkingLabel(t *testing.T) {
	render := func(a AgentResult) string {
		var buf bytes.Buffer
		RenderTable(&buf, &Report{Agents: []AgentResult{a}})
		return buf.String()
	}
	out := render(AgentResult{Agent: "a", Status: StatusOK, ThinkingStatus: ThinkingNotHonored, ThinkingDetail: "32 reasoning tokens"})
	assert.Contains(t, out, "thinking not honored: 32 reasoning tokens")

	out = render(AgentResult{Agent: "a", Status: StatusOK, ThinkingStatus: ThinkingUnverified, ThinkingDetail: "no signal"})
	assert.Contains(t, out, "thinking unverified: no signal")

	out = render(AgentResult{Agent: "a", Status: StatusOK,
		ResponseFormatStatus: ResponseFormatNotHonored, ResponseFormatDetail: "fenced",
		ThinkingStatus: ThinkingNotHonored, ThinkingDetail: "tokens"})
	rf, th := strings.Index(out, "response_format not honored: fenced"), strings.Index(out, "thinking not honored: tokens")
	require.True(t, rf >= 0 && th >= 0, out)
	assert.Less(t, rf, th, "thinking rides after response_format")

	out = render(AgentResult{Agent: "a", Status: StatusOK, ThinkingStatus: ThinkingNotHonored, ThinkingDetail: strings.Repeat("x", 400)})
	assert.Contains(t, out, "… (--json for full text)")
	assert.NotContains(t, out, strings.Repeat("x", 161))

	// An honored verdict can carry a detail (the leveled "reasoning observed" text);
	// it must not leak into the HINT cell unlabeled.
	out = render(AgentResult{Agent: "a", Status: StatusOK, ThinkingStatus: ThinkingHonored, ThinkingDetail: "reasoning observed; the probe does not verify thinking_level low"})
	assert.NotContains(t, out, "reasoning observed")
	assert.NotContains(t, out, "thinking")
}

// Sprint 35.16.11.2.2.1 AC 03-04 Edge Case 3: preserve_thinking joins a
// declared target's identity, and both doctor probe sites send it.
func TestDoctor_PreserveThinkingJoinsTargetAndProbe(t *testing.T) {
	on := registry.AgentConfig{Thinking: "on", ThinkingStyle: "qwen"}
	flagged := on
	flagged.PreserveThinking = "on"
	res := declaredRegistry(t, map[string]registry.AgentConfig{"a": flagged, "b": on})
	assert.Len(t, res.Targets, 2, "the flag changes the request, so it splits the target")
	assert.Equal(t, "on", targetForAgent(t, res, "a").PreserveThinking)
	assert.Empty(t, targetForAgent(t, res, "b").PreserveThinking)
	assert.Len(t, declaredRegistry(t, map[string]registry.AgentConfig{"a": flagged, "b": flagged}).Targets, 1)

	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m", ResponseFormat: registry.ResponseFormatJSONObject,
			Thinking: registry.ThinkingOn, ThinkingStyle: registry.ThinkingStyleGLM, PreserveThinking: registry.ThinkingOn,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	t.Setenv(rfDoctorEnvK, thinkingKey)
	fake := newFake(markerOK)
	// TD internal/doctor/thinking_test.go:500: the old script returned the
	// `thinks` fixture for every call, so probeThinking reached honored on the
	// declared call alone — the loop over calls[1:] never ran and the "control
	// call drops the whole declaration" claim was checked by an empty loop.
	// Script a silent declared call reporting no reasoning-token field, which
	// forces the control call, then pin the control's PreserveThinking exactly.
	n := 0
	fake.metaFn = func(llmclient.Invocation) (llmclient.Completion, error) {
		n++
		if n == 1 {
			return withMarker(llmclient.Completion{}), nil // silent, not reported-zero
		}
		return thinks, nil
	}
	fake.chatFn = reply(oneFinding)
	Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	calls := fake.completeCalls()
	require.Len(t, calls, 2, "the silent declared call must force exactly one control call")
	assert.Equal(t, registry.ThinkingOn, calls[0].PreserveThinking, "the marker probe sends the flag")
	assert.Empty(t, calls[1].PreserveThinking, "the control call drops the whole declaration")
	chats := fake.chatCalls()
	require.Len(t, chats, 1)
	assert.Equal(t, registry.ThinkingOn, chats[0].inv.PreserveThinking, "the response_format probe sends the flag")
}

// TD-012 / TD internal/doctor/run.go:897: the probe is single-turn — an
// honored verdict only proves the flag was accepted, not that reasoning was
// actually preserved. When the target sends the flag, the honored detail must
// say so.
func TestRun_HonoredDetailNotesPreserveThinkingUnverified(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m",
			Thinking: registry.ThinkingOn, ThinkingStyle: registry.ThinkingStyleQwen, PreserveThinking: registry.ThinkingOn,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	a, _, _ := runThinking(t, res, thinks, nil, llmclient.Completion{}, nil)
	assert.Equal(t, ThinkingHonored, a.ThinkingStatus)
	assert.Contains(t, a.ThinkingDetail, "the probe does not verify preserve_thinking (single-turn)")
}

// TD cli/doctor.go:367: a provider 4xx on the flagged marker call must leave
// the flag visible on the result row, so the cli warning and --json consumers
// can name "retry without preserve_thinking" without parsing detail prose.
func TestRun_ThinkingPreserveFieldPopulatedOnFlaggedRejection(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m",
			Thinking: registry.ThinkingOn, ThinkingStyle: registry.ThinkingStyleQwen, PreserveThinking: registry.ThinkingOn,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	a, _, _ := runThinking(t, res, llmclient.Completion{}, &llmclient.HTTPStatusError{Status: 400, Snippet: "preserve_thinking not supported"}, silent, nil)
	assert.Equal(t, ThinkingNotHonored, a.ThinkingStatus)
	assert.Equal(t, registry.ThinkingOn, a.ThinkingPreserve, "the flagged probe's preserve_thinking must land on the result row")
	assert.Contains(t, a.ThinkingDetail, "preserve_thinking")
}

// TD internal/doctor/run.go:1049: a level-alone target that also sends
// preserve_thinking already has a thinking_level detail when honored, so the
// preserve note is APPENDED rather than set. Pin the joined sentence exactly.
func TestRun_HonoredDetailAppendsPreserveNoteToLevelDetail(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m",
			ThinkingLevel: registry.ThinkingLevelLow, ThinkingStyle: registry.ThinkingStyleQwen, PreserveThinking: registry.ThinkingOn,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	a, _, _ := runThinking(t, res, thinks, nil, llmclient.Completion{}, nil)
	assert.Equal(t, ThinkingHonored, a.ThinkingStatus)
	assert.Equal(t, "reasoning observed; the probe does not verify thinking_level low; the probe does not verify preserve_thinking (single-turn)", a.ThinkingDetail)
}

// TD internal/doctor/run.go:931: thinking_preserve is omitted exactly when
// thinking_declared is. A preserve_thinking target whose call reaches no
// verdict (a permanent 401) must not report the flag on its row.
func TestRun_ThinkingPreserveOmittedWithoutAVerdict(t *testing.T) {
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m",
			Thinking: registry.ThinkingOn, ThinkingStyle: registry.ThinkingStyleQwen, PreserveThinking: registry.ThinkingOn,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)

	a, _, _ := runThinking(t, res, llmclient.Completion{}, &llmclient.HTTPStatusError{Status: 401, Snippet: "bad key"}, llmclient.Completion{}, nil)
	assert.Empty(t, a.ThinkingStatus)
	assert.Empty(t, a.ThinkingPolarity)
	assert.Empty(t, a.ThinkingPreserve, "thinking_preserve is omitted exactly when thinking_declared is")
}

// TD-010: the verdict label names preserve_thinking when the target sends it,
// so a flag-caused rejection is not blamed on thinking alone.
func TestThinkingDeclaration_NamesPreserveThinking(t *testing.T) {
	assert.Equal(t, "thinking: on (glm)", thinkingDeclaration(Target{Thinking: "on", ThinkingStyle: "glm"}))
	assert.Equal(t, "thinking: on (glm), preserve_thinking: on",
		thinkingDeclaration(Target{Thinking: "on", ThinkingStyle: "glm", PreserveThinking: "on"}))
	assert.Equal(t, "thinking_level: low (qwen), preserve_thinking: off",
		thinkingDeclaration(Target{ThinkingLevel: "low", ThinkingStyle: "qwen", PreserveThinking: "off"}))
}

// TD internal/registry/config.go:1515: the evidence wording and the HINT
// separator were asserted only with Contains, so a wording change passed the
// suite while every operator-facing doc and alert regex drifted. These pin the
// EXACT strings: the full honored-off detail sentence (run.go evidence clause)
// and the leading " | " separator render.go inserts between labels.
func TestRun_HonoredOffDetailExactWording(t *testing.T) {
	a, _, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), silent, nil, llmclient.Completion{}, nil)
	assert.Equal(t, ThinkingUnverified, a.ThinkingStatus)
	// The both-silent case pins the control-probe evidence wording exactly.
	a2, _, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), silent, nil, silent, nil)
	assert.Equal(t, ThinkingUnverified, a2.ThinkingStatus)
	assert.Equal(t,
		"no reasoning signal under thinking: off (qwen), and none from a control probe without the declaration either (no reasoning tokens, no reasoning content), so the provider may not report reasoning at all",
		a2.ThinkingDetail)
}

func TestRenderTable_ThinkingLabelExactSeparator(t *testing.T) {
	var buf bytes.Buffer
	RenderTable(&buf, &Report{Agents: []AgentResult{{
		Agent: "a", Status: StatusOK,
		ResponseFormatStatus: ResponseFormatNotHonored, ResponseFormatDetail: "fenced",
		ThinkingStatus: ThinkingNotHonored, ThinkingDetail: "tokens",
	}}})
	out := buf.String()
	sep := " | thinking not honored: tokens"
	assert.Contains(t, out, sep, "the second label must ride the exact leading separator")
	assert.NotContains(t, out, " | thinking not honored: tokens |", "no trailing separator after the last label")
}
