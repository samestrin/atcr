# Testing & Fixtures [REFERENCE]

## Overview

Epic 3.0 requires comprehensive testing of the verification pipeline, including unit tests for verdict parsing, integration tests for skeptic invocation, and end-to-end tests with a fixture corpus containing planted true/false findings. Tests use the **testify** assertion library and follow the established golden file pattern in `internal/report/testdata/`.

> Source: [original-requirements.md:Quality criteria]
> Source: [testify.md:Assertion patterns]

---

## Key Concepts

### Testify Assertion Patterns

- Use `assert` for non-critical assertions (test continues on failure).
- Use `require` for critical assertions (test halts on failure).
- Use `suite` for grouping related tests with shared setup/teardown.

> Source: [testify.md:Core API]

### Golden File Testing

- Location: `internal/report/testdata/` (existing pattern)
- Files:
  - `findings.json` — input fixture
  - `report.md` — golden output for `Render()` tests
  - `checklist.md` — golden output for checklist format
- Epic 3.0 additions:
  - New variant: `findings-with-verification.json` — input fixture with verification blocks
  - Update `report.md` to include VERIFIED tier and collapsed Refuted section
  - Update `checklist.md` for confidence v2 tiers

> Source: [codebase-discovery.json:existing_patterns → Report test fixtures]
> Source: [codebase-discovery.json:integration_gaps → Report golden file cascade]

### Fixture Corpus

- Location: `internal/verify/testdata/` (new directory)
- Contents:
  - `true-finding.json` — a deliberately true finding (plausible and correct) that should be **confirmed** by skeptics
  - `false-finding.json` — a deliberately false finding (plausible but wrong) that should be **refuted** by skeptics
  - `malformed-response.txt` — a malformed skeptic response (invalid JSON) to test the fallback path
  - `mock-skeptic.go` — a scripted mock skeptic that refutes the false finding and confirms the true finding

> Source: [original-requirements.md:Quality criteria → End-to-end fixture]

### Verdict Parsing Tests

- Location: `internal/verify/verdict_test.go` (new file)
- Test cases:
  1. Valid JSON with `confirmed` verdict
  2. Valid JSON with `refuted` verdict
  3. Valid JSON with `unverifiable` verdict
  4. Malformed JSON (missing closing brace)
  5. Invalid verdict enum (e.g., `"maybe"`)
  6. Empty response
  7. Response with extra fields (should be ignored)

> Source: [original-requirements.md:Quality criteria → Skeptic envelope parsing]

### Skeptic Invocation Tests

- Location: `internal/verify/verify_test.go` (new file)
- Test cases:
  1. Skeptic selection with different-model rule
  2. No eligible skeptic → `unverifiable` with `no_eligible_skeptic`
  3. Single skeptic confirms finding → VERIFIED
  4. Single skeptic refutes finding → LOW
  5. Three skeptics, majority confirms → VERIFIED
  6. Three skeptics, disagreeing verdicts → `unverifiable` with all reasonings
  7. Budget tripped → `unverifiable` with `budget_exceeded`
  8. Provider error → `unverifiable` (run does not fail)

> Source: [original-requirements.md:Functional criteria]

### End-to-End Tests

- Location: `cmd/atcr/verify_test.go` (new file)
- Test cases:
  1. `atcr verify` runs successfully, produces `verification.json`
  2. `atcr verify --fresh` re-verifies all findings
  3. `atcr verify --thorough` uses 3 skeptics
  4. `atcr verify --min-severity HIGH` skips MEDIUM/LOW findings
  5. `atcr review --verify` chains review → reconcile → verify
  6. `--fail-on high` excludes refuted findings
  7. `--fail-on high --require-verified` counts only VERIFIED findings

> Source: [original-requirements.md:Functional criteria]

### Gate Semantics Tests

- Location: `internal/reconcile/gate_test.go` (extend existing)
- Test cases:
  1. `CountAtOrAbove` excludes refuted findings
  2. `CountVerified` counts only VERIFIED findings
  3. `--fail-on high` with mix of VERIFIED/HIGH/MEDIUM/LOW
  4. `--require-verified` with no VERIFIED findings → count = 0

> Source: [original-requirements.md:Functional criteria → Gate semantics]

---

## Code Examples

### Verdict Parsing Test (Table-Driven)

```go
// From internal/verify/verdict_test.go (new file)
func TestParseVerdict(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected *reconcile.Verification
    }{
        {
            name:  "confirmed",
            input: `{"verdict": "confirmed", "reasoning": "evidence checks out"}`,
            expected: &reconcile.Verification{
                Verdict: "confirmed",
                Notes:   "evidence checks out",
            },
        },
        {
            name:  "refuted",
            input: `{"verdict": "refuted", "reasoning": "code path unreachable"}`,
            expected: &reconcile.Verification{
                Verdict: "refuted",
                Notes:   "code path unreachable",
            },
        },
        {
            name:  "malformed_json",
            input: `{"verdict": "confirmed", "reasoning": "missing closing brace"`,
            expected: &reconcile.Verification{
                Verdict: "unverifiable",
                Notes:   "malformed_output: " + `{"verdict": "confirmed", "reasoning": "missing closing brace"`,
            },
        },
        {
            name:  "invalid_verdict",
            input: `{"verdict": "maybe", "reasoning": "unsure"}`,
            expected: &reconcile.Verification{
                Verdict: "unverifiable",
                Notes:   "invalid_verdict: maybe (raw: " + `{"verdict": "maybe", "reasoning": "unsure"}` + ")",
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := parseVerdict(tt.input)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

> Source: [testify.md:Table-driven tests]

### Golden File Test (Report Rendering)

```go
// From internal/report/render_test.go (extend existing)
func TestRenderWithVerification(t *testing.T) {
    // Load fixture with verification blocks
    findings, err := reconcile.ReadReconciledFindings("testdata/findings-with-verification.json")
    require.NoError(t, err)

    // Render report
    var buf bytes.Buffer
    err = Render(&buf, findings, FormatMarkdown)
    require.NoError(t, err)

    // Compare to golden file
    expected, err := os.ReadFile("testdata/report.md")
    require.NoError(t, err)

    assert.Equal(t, string(expected), buf.String())
}
```

> Source: [testify.md:Golden file testing]

### Mock Skeptic (Fixture)

```go
// From internal/verify/testdata/mock-skeptic.go (new file)
// MockSkeptic implements fanout.ChatCompleter so the verify package can drive
// it through the real tool loop without network calls.
type MockSkeptic struct {
    ConfirmFindings []string // finding IDs to confirm
    RefuteFindings  []string // finding IDs to refute
}

func (m *MockSkeptic) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
    return "", fmt.Errorf("mock skeptic is tool-loop only")
}

func (m *MockSkeptic) Chat(ctx context.Context, inv llmclient.Invocation, messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
    // Extract finding ID from the user prompt in messages.
    findingID := extractFindingID(messages)

    var verdict string
    if contains(m.ConfirmFindings, findingID) {
        verdict = "confirmed"
    } else if contains(m.RefuteFindings, findingID) {
        verdict = "refuted"
    } else {
        verdict = "unverifiable"
    }

    content := fmt.Sprintf(`{"verdict": "%s", "reasoning": "mock skeptic response"}`, verdict)
    return &llmclient.ChatResponse{
        Message: llmclient.Message{Role: "assistant", Content: &content},
    }, nil
}
```

> Source: [original-requirements.md:Quality criteria → End-to-end fixture]

---

## Quick Reference

| Test Type | Location | Notes |
|-----------|----------|-------|
| Verdict parsing | `internal/verify/verdict_test.go` | Table-driven, malformed output |
| Skeptic invocation | `internal/verify/verify_test.go` | Different-model rule, vote mechanics |
| Gate semantics | `internal/reconcile/gate_test.go` | Refuted exclusion, `--require-verified` |
| CLI integration | `cmd/atcr/verify_test.go` | Flags, chaining, gate status |
| Report rendering | `internal/report/render_test.go` | Golden files, v2 tiers |
| Fixture corpus | `internal/verify/testdata/` | True/false findings, mock skeptic |

---

## Related Documentation

- [Verification Pipeline Architecture](verification-pipeline.md) — core mechanics tested by verdict parsing tests
- [CLI & MCP Integration](cli-mcp-integration.md) — CLI and MCP integration tests
- [LLM Integration & Tool Loop](llm-tool-loop.md) — skeptic invocation tests
