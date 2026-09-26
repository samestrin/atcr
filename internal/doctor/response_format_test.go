package doctor

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	oneFinding   = `{"findings":[{"severity":"HIGH","file_line":"probe.go:2","problem":"division by zero","fix":"check b","category":"correctness","est_minutes":5,"evidence":"return a / b"}]}`
	rfDoctorKey  = "sk-rf-secret"
	rfDoctorEnvK = "ATCR_DOCTOR_KEY"
)

// declaredTarget resolves one agent declaring response_format, optionally as a
// tool-loop agent.
func declaredTarget(t *testing.T, tools bool) *Resolution {
	t.Helper()
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "m",
			ResponseFormat: registry.ResponseFormatJSONObject,
			Tools:          tools, SupportsFC: tools,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	return res
}

func markerOK(llmclient.Invocation) (string, error) { return Marker(testNonce), nil }

func reply(content string) func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	return func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &content}}, nil
	}
}

func rejected(status int, snippet string) func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	return func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		return nil, &llmclient.HTTPStatusError{Status: status, Snippet: snippet}
	}
}

func runDeclared(t *testing.T, tools bool, chat func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error)) (AgentResult, *fakeCompleter) {
	t.Helper()
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	fake := newFake(markerOK)
	fake.chatFn = chat
	rep := Run(context.Background(), fake, declaredTarget(t, tools), Options{Nonce: testNonce, MaxTokens: 2048})
	require.Len(t, rep.Agents, 1)
	return rep.Agents[0], fake
}

// A bare findings object is what JSON mode guarantees, and one finding is what the
// planted defect should produce.
func TestRun_ResponseFormatProbePassesOnABareFindingsObject(t *testing.T) {
	a, fake := runDeclared(t, false, reply(oneFinding))

	assert.Equal(t, StatusOK, a.Status, "the marker-echo verdict is untouched")
	assert.Equal(t, ResponseFormatHonored, a.ResponseFormatStatus)

	calls := fake.chatCalls()
	require.Len(t, calls, 1, "one findings-object probe, no combined probe for a non-tool agent")
	assert.Equal(t, registry.ResponseFormatJSONObject, calls[0].inv.ResponseFormat,
		"the probe must send the invocation it claims to test")
	assert.Empty(t, calls[0].tools)
	// The probe must reproduce the invocation it speaks for: the run's output cap
	// and the endpoint identity ride along, not just the response_format field —
	// otherwise deleting the cap (an uncapped probe) or the endpoint routing passes
	// the suite while the probe stops being evidence about the real call.
	require.NotNil(t, calls[0].inv.MaxTokens, "the probe carries the run's output cap")
	assert.Equal(t, 2048, *calls[0].inv.MaxTokens)
	assert.Equal(t, "m", calls[0].inv.Model)
	assert.Equal(t, "https://api.example/v1", calls[0].inv.BaseURL)
	assert.Equal(t, rfDoctorEnvK, calls[0].inv.APIKeyEnv,
		"the probe routes through the target's credential env var, as the client resolves it")
	var mentionsJSON bool
	for _, m := range calls[0].msgs {
		if m.Content != nil && strings.Contains(*m.Content, "JSON") {
			mentionsJSON = true
		}
	}
	assert.True(t, mentionsJSON, "some providers reject json_object unless a message names JSON")
}

// The response_format probe applies the run's per-call timeout: a hung upstream
// must yield an unverified verdict on a deadline, not stall the whole self-test.
func TestRun_ResponseFormatProbeAppliesPerCallTimeout(t *testing.T) {
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	fake := newFake(markerOK)
	fake.chatFn = reply(oneFinding)

	Run(context.Background(), fake, declaredTarget(t, false),
		Options{Nonce: testNonce, MaxTokens: 2048, Timeout: 5 * time.Second})

	calls := fake.chatCalls()
	require.Len(t, calls, 1)
	assert.True(t, calls[0].ctxHasDeadline,
		"the response_format probe must forward a context with the Options timeout deadline")
}

// responseFormatPrompt restates the findings contract that fanout's shared
// ## Output Format block sends review agents — one contract in two lanes. This
// drift test derives the key and severity lists FROM the fanout constant, so a key
// added or renamed there without the doctor prompt following fails here instead of
// silently probing a different contract than the review actually runs.
func TestResponseFormatPromptMatchesTheFanoutContract(t *testing.T) {
	keyLineRe := regexp.MustCompile(`(?m)^"severity".*$`)
	line := keyLineRe.FindString(fanout.JsonObjectOutputFormat)
	require.NotEmpty(t, line, "the fanout block's key list line moved — update this test")
	for _, m := range regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(line, -1) {
		assert.Containsf(t, responseFormatPrompt, m[1],
			"fanout contract key %q is missing from the doctor's response_format prompt", m[1])
	}

	sevLineRe := regexp.MustCompile(`(?m)^Rules: severity is one of (.*?);`)
	sevLine := sevLineRe.FindStringSubmatch(fanout.JsonObjectOutputFormat)
	require.Len(t, sevLine, 2, "the fanout block's severity rule line moved — update this test")
	for _, sev := range strings.Split(sevLine[1], ", ") {
		assert.Containsf(t, responseFormatPrompt, sev,
			"fanout contract severity %q is missing from the doctor's response_format prompt", sev)
	}
}

// ok_warning (HTTP 200, marker absent — the thinking-model signature) still counts
// as a working endpoint for the probe gate: the truncated/unverified response_format
// paths target exactly these agents, so narrowing the gate to a bare ok would
// silently skip every one of them.
func TestRun_ResponseFormatProbeRunsForAnOkWarningEndpoint(t *testing.T) {
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	fake := newFake(func(llmclient.Invocation) (string, error) {
		return "reasoning... marker lost", nil
	})
	fake.chatFn = reply(oneFinding)

	rep := Run(context.Background(), fake, declaredTarget(t, false), Options{Nonce: testNonce, MaxTokens: 2048})
	require.Len(t, rep.Agents, 1)

	assert.Equal(t, StatusOKWarning, rep.Agents[0].Status)
	require.Len(t, fake.chatCalls(), 1, "the response_format probe must run for an ok_warning endpoint")
	assert.Equal(t, ResponseFormatHonored, rep.Agents[0].ResponseFormatStatus)
}

// The marker probe must not carry response_format: its "Reply with exactly" prompt
// cannot be answered as a JSON object.
func TestRun_MarkerProbeNeverSendsResponseFormat(t *testing.T) {
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	var seen []string
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		seen = append(seen, inv.ResponseFormat)
		return Marker(testNonce), nil
	})
	fake.chatFn = reply(oneFinding)

	Run(context.Background(), fake, declaredTarget(t, false), Options{Nonce: testNonce, MaxTokens: 2048})

	require.Len(t, seen, 1)
	assert.Empty(t, seen[0])
}

// {"findings":[]} is the required object; an empty array is a clean review, not a
// broken one.
func TestRun_ResponseFormatProbePassesOnACleanFindingsObject(t *testing.T) {
	a, _ := runDeclared(t, false, reply(`{"findings":[]}`))

	assert.Equal(t, ResponseFormatHonored, a.ResponseFormatStatus)
}

// Each of these is a reply JSON mode cannot produce, so the provider either ignored
// the field or broke it. The parser accepts some of them (fence, prose), which is
// exactly why the strict bare-object check exists.
func TestRun_ResponseFormatProbeWarnsWhenTheReplyIsNotABareObject(t *testing.T) {
	cases := map[string]string{
		"fenced object":  "```json\n" + oneFinding + "\n```",
		"prose before":   "Here are the findings:\n" + oneFinding,
		"plain sentinel": "NO FINDINGS",
		"prose only":     "The code divides by zero when b is 0.",
		"bare array":     "[]",
		"empty":          "",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := runDeclared(t, false, reply(content))

			assert.Equal(t, StatusOK, a.Status, "a response_format mismatch never changes the endpoint verdict")
			assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
			assert.NotEmpty(t, a.ResponseFormatDetail)
		})
	}
}

// A bare object the parser cannot read as findings is valid JSON, but not the
// {"findings":[...]} object the review lane needs.
func TestRun_ResponseFormatProbeWarnsOnAnObjectThatIsNotFindings(t *testing.T) {
	a, _ := runDeclared(t, false, reply(`{"answer":"division by zero"}`))

	assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
}

// A provider rejecting the field is the same warning class, with its own detail, and
// the upstream snippet is scrubbed of the key like every other detail.
func TestRun_ResponseFormatProbeWarnsWhenTheProviderRejectsTheField(t *testing.T) {
	a, _ := runDeclared(t, false, rejected(400, "response_format unsupported for key "+rfDoctorKey))

	assert.Equal(t, StatusOK, a.Status)
	assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
	assert.Contains(t, a.ResponseFormatDetail, "400")
	assert.Contains(t, a.ResponseFormatDetail, "rejected")
	assert.NotContains(t, a.ResponseFormatDetail, rfDoctorKey, "details pass through scrubCredentials")
}

// A rate limit, a 5xx, a timeout, or a transport error says nothing about whether the
// field is honored. Calling it not_honored would tell the operator to drop a working
// declaration, and quota-limited upstreams make a 429 on this second call likely.
func TestRun_ResponseFormatProbeIsUnverifiedOnATransientError(t *testing.T) {
	cases := map[string]error{
		"rate limited": &llmclient.HTTPStatusError{Status: 429, Snippet: "quota"},
		// 408 is the provider timing out on the probe — the same no-verdict class as
		// a client-side deadline, never a verdict on the declaration.
		"request timeout": &llmclient.HTTPStatusError{Status: 408, Snippet: "probe timed out"},
		"server error":    &llmclient.HTTPStatusError{Status: 503, Snippet: "upstream down"},
		"deadline":        context.DeadlineExceeded,
		"transport":       errors.New("connection reset"),
	}
	for name, callErr := range cases {
		t.Run(name, func(t *testing.T) {
			a, _ := runDeclared(t, false, func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
				return nil, callErr
			})

			assert.Equal(t, StatusOK, a.Status)
			assert.Equal(t, ResponseFormatUnverified, a.ResponseFormatStatus)
			assert.NotContains(t, a.ResponseFormatDetail, "rejected")
		})
	}
}

// A reply cut off at the cap is invalid JSON no matter what the provider did with the
// field, so it reaches no verdict rather than blaming the model.
func TestRun_ResponseFormatProbeIsUnverifiedWhenTheReplyIsTruncated(t *testing.T) {
	a, _ := runDeclared(t, false, func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		cut := `{"findings":[{"severity":"HIGH","file_li`
		return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &cut}, FinishReason: "length", Truncated: true}, nil
	})

	assert.Equal(t, ResponseFormatUnverified, a.ResponseFormatStatus)
	assert.Contains(t, a.ResponseFormatDetail, "cut off")
	assert.NotContains(t, a.ResponseFormatDetail, "ignored response_format")
}

// A definite failure outranks an inconclusive one: the operator must hear about it.
func TestRun_NotHonoredOutranksUnverifiedAcrossProbes(t *testing.T) {
	a, _ := runDeclared(t, true, func(inv llmclient.Invocation, msgs []llmclient.Message, tools []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		if len(tools) > 0 {
			return rejected(400, "tools and response_format cannot be combined")(inv, msgs, tools)
		}
		return rejected(429, "quota")(inv, msgs, tools)
	})

	assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
}

// JSON mode held but the object carries no readable findings: the detail must not
// blame response_format for a contract the model ignored.
func TestRun_ResponseFormatProbeDoesNotBlameTheFieldForAWrongShapeObject(t *testing.T) {
	a, _ := runDeclared(t, false, reply(`{"findings":[{"severity":"major","problem":"x"}]}`))

	assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
	assert.NotContains(t, a.ResponseFormatDetail, "ignored response_format")
}

// The prompt names the severity values, or a model under JSON mode can answer with a
// severity the parser drops and fail the probe for a reason unrelated to the field.
func TestRun_ResponseFormatPromptNamesTheSeverityValues(t *testing.T) {
	_, fake := runDeclared(t, false, reply(oneFinding))

	calls := fake.chatCalls()
	require.NotEmpty(t, calls)
	require.NotNil(t, calls[0].msgs[0].Content)
	assert.Contains(t, *calls[0].msgs[0].Content, "CRITICAL, HIGH, MEDIUM, LOW")
}

// An undeclared agent pays nothing and reports nothing new.
func TestRun_UndeclaredAgentRunsNoResponseFormatProbe(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	fake := newFake(markerOK)

	rep := Run(context.Background(), fake, twoAgentSharedTarget(t), Options{Nonce: testNonce, MaxTokens: 2048})

	assert.Empty(t, fake.chatCalls())
	for _, a := range rep.Agents {
		assert.Empty(t, a.ResponseFormatStatus)
		assert.Empty(t, a.ResponseFormatDetail)
	}
}

// When the endpoint itself failed, a JSON-mode probe can only fail the same way and
// would bury the real cause under a second warning.
func TestRun_ResponseFormatProbeSkippedWhenTheEndpointFailed(t *testing.T) {
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	fake := newFake(func(llmclient.Invocation) (string, error) {
		return "", &llmclient.HTTPStatusError{Status: 401}
	})
	fake.chatFn = reply(oneFinding)

	rep := Run(context.Background(), fake, declaredTarget(t, false), Options{Nonce: testNonce, MaxTokens: 2048})

	require.Len(t, rep.Agents, 1)
	assert.Equal(t, StatusAuthFailed, rep.Agents[0].Status)
	assert.Empty(t, fake.chatCalls())
	assert.Empty(t, rep.Agents[0].ResponseFormatStatus)
}

// The combined probe sends both declarations in one request, the pair the real tool
// loop sends on every turn.
func TestRun_CombinedProbePassesOnAToolCall(t *testing.T) {
	a, fake := runDeclared(t, true, func(inv llmclient.Invocation, _ []llmclient.Message, tools []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		if len(tools) == 0 {
			return reply(oneFinding)(inv, nil, nil)
		}
		return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", ToolCalls: []llmclient.ToolCall{{
			ID: "c1", Type: "function", Function: llmclient.FunctionCall{Name: tools[0].Name, Arguments: []byte("{}")},
		}}}}, nil
	})

	assert.Equal(t, ResponseFormatHonored, a.ResponseFormatStatus)
	calls := fake.chatCalls()
	require.Len(t, calls, 2, "the findings-object probe plus the combined probe")
	var combined *chatCall
	for i := range calls {
		if len(calls[i].tools) > 0 {
			combined = &calls[i]
		}
	}
	require.NotNil(t, combined, "one call must carry the tool definition")
	assert.Len(t, combined.tools, 1)
	assert.Equal(t, registry.ResponseFormatJSONObject, combined.inv.ResponseFormat)
}

// Declining the tool is fine; the pass condition is a tool call OR a findings object.
func TestRun_CombinedProbePassesOnAFindingsObjectWithoutAToolCall(t *testing.T) {
	a, _ := runDeclared(t, true, reply(oneFinding))

	assert.Equal(t, ResponseFormatHonored, a.ResponseFormatStatus)
}

func TestRun_CombinedProbeWarnsWhenTheCombinationIsRejected(t *testing.T) {
	a, _ := runDeclared(t, true, func(inv llmclient.Invocation, msgs []llmclient.Message, tools []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		if len(tools) > 0 {
			return rejected(400, "tools and response_format cannot be combined")(inv, msgs, tools)
		}
		return reply(oneFinding)(inv, msgs, tools)
	})

	assert.Equal(t, StatusOK, a.Status)
	assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
	assert.Contains(t, a.ResponseFormatDetail, "tools", "name both declarations so the operator knows which pair failed")
	assert.Contains(t, a.ResponseFormatDetail, "response_format")
	assert.Contains(t, a.ResponseFormatDetail, "rejected")
}

// Accepted but answered with neither shape: same class, different detail from an
// outright rejection.
func TestRun_CombinedProbeWarnsWhenTheReplyIsNeitherShape(t *testing.T) {
	neither := func(inv llmclient.Invocation, msgs []llmclient.Message, tools []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		if len(tools) > 0 {
			return reply("I would call the tool here.")(inv, msgs, tools)
		}
		return reply(oneFinding)(inv, msgs, tools)
	}
	a, _ := runDeclared(t, true, neither)

	assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
	assert.Contains(t, a.ResponseFormatDetail, "tools")
	assert.NotContains(t, a.ResponseFormatDetail, "rejected", "an ignored field reads differently from a rejected one")
}

func TestRun_CombinedProbeWarnsOnAFencedObjectWithoutAToolCall(t *testing.T) {
	fenced := func(inv llmclient.Invocation, msgs []llmclient.Message, tools []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		if len(tools) > 0 {
			return reply("```json\n"+oneFinding+"\n```")(inv, msgs, tools)
		}
		return reply(oneFinding)(inv, msgs, tools)
	}
	a, _ := runDeclared(t, true, fenced)

	assert.Equal(t, ResponseFormatNotHonored, a.ResponseFormatStatus)
}

// A declared agent without the tool loop gets only the findings-object probe.
func TestRun_CombinedProbeSkippedForADeclaredNonToolAgent(t *testing.T) {
	_, fake := runDeclared(t, false, reply(oneFinding))

	// Guard the loop: zero calls would otherwise make "no combined probe"
	// indistinguishable from "no probe at all".
	calls := fake.chatCalls()
	require.Len(t, calls, 1, "the plain probe runs; only the combined probe is skipped")
	for _, c := range calls {
		assert.Empty(t, c.tools, "no tool definition is sent for an agent that runs no tool loop")
	}
}

// A tool-loop agent that did not declare response_format runs neither probe.
func TestRun_CombinedProbeSkippedForAnUndeclaredToolAgent(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", Tools: true, SupportsFC: true}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	fake := newFake(markerOK)

	Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})

	assert.Empty(t, fake.chatCalls())
}

// Both new fields are omitempty, so an undeclared agent's --json row is unchanged.
func TestRenderJSON_UndeclaredAgentCarriesNoResponseFormatKeys(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{{
		Agent: "a", Provider: "p", Model: "m", Status: StatusOK, MaxTokens: 2048,
	}}}))

	assert.NotContains(t, buf.String(), "response_format")
}

func TestRenderJSON_DeclaredAgentCarriesTheProbeOutcome(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{{
		Agent: "a", Provider: "p", Model: "m", Status: StatusOK,
		ResponseFormatStatus: ResponseFormatNotHonored, ResponseFormatDetail: "reply was fenced",
	}}}))

	assert.Contains(t, buf.String(), `"response_format_status": "not_honored"`)
	assert.Contains(t, buf.String(), `"response_format_detail": "reply was fenced"`)
}

// Both diagnostic branches the short-detail tests miss: an endpoint hint and a
// response_format detail must join under the " | " separator (never read as one
// sentence), and an over-160-byte detail must clamp with the --json suffix — the
// wrong-shape detail at run.go already exceeds the cap in practice.
func TestRenderTable_JoinsHintAndClampsLongResponseFormatDetail(t *testing.T) {
	long := strings.Repeat("x", 200)
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "a", Provider: "p", Model: "m", Status: StatusOKWarning,
		Hint:                 "HTTP 200 but marker absent/empty",
		ResponseFormatStatus: ResponseFormatNotHonored,
		ResponseFormatDetail: long,
	}}}))

	out := buf.String()
	assert.Contains(t, out, "HTTP 200 but marker absent/empty | response_format not honored: ",
		"the endpoint hint and the labelled detail join under the pipe separator")
	assert.Contains(t, out, "… (--json for full text)",
		"a detail over the 160-byte table cap clamps with the --json suffix")
	assert.NotContains(t, out, strings.Repeat("x", 161), "the clamped cell never carries the full detail")
}

// The table must show a not-honored row in words that cannot be mistaken for the
// marker-absent hint.
func TestRenderTable_NotHonoredRowNamesResponseFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "a", Provider: "p", Model: "m", Status: StatusOK,
		ResponseFormatStatus: ResponseFormatNotHonored, ResponseFormatDetail: "reply was fenced",
	}}}))

	assert.Contains(t, buf.String(), "response_format not honored")
	assert.Contains(t, buf.String(), "reply was fenced")
}

func TestRenderTable_UnverifiedRowIsLabelledUnverified(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "a", Provider: "p", Model: "m", Status: StatusOK,
		ResponseFormatStatus: ResponseFormatUnverified, ResponseFormatDetail: "HTTP 429",
	}}}))

	assert.Contains(t, buf.String(), "response_format unverified: HTTP 429")
	assert.NotContains(t, buf.String(), "not honored")
}

func TestRenderTable_HonoredRowAddsNothing(t *testing.T) {
	var honored, plain bytes.Buffer
	row := AgentResult{Agent: "a", Provider: "p", Model: "m", Status: StatusOK}
	require.NoError(t, RenderTableError(&plain, &Report{Agents: []AgentResult{row}}))
	row.ResponseFormatStatus = ResponseFormatHonored
	require.NoError(t, RenderTableError(&honored, &Report{Agents: []AgentResult{row}}))

	assert.Equal(t, plain.String(), honored.String(), "a healthy row stays quiet")
}
