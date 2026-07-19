package mcp

import (
	"context"
	"slices"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listTools returns the registered tools by name.
func listTools(t *testing.T, cs *mcpsdk.ClientSession) map[string]*mcpsdk.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	byName := make(map[string]*mcpsdk.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	return byName
}

func TestToolRegistration_Count(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	assert.Len(t, listTools(t, cs), 8)
}

func TestToolRegistration_Names(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	tools := listTools(t, cs)
	for _, want := range []string{ToolReview, ToolReconcile, ToolVerify, ToolDebate, ToolReport, ToolRange, ToolStatus, ToolMetrics} {
		_, ok := tools[want]
		assert.True(t, ok, "tool %q must be registered", want)
	}
}

func TestToolRegistration_Descriptions(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	for name, tool := range listTools(t, cs) {
		assert.NotEmpty(t, tool.Description, "tool %q must have a non-empty description", name)
	}
}

// TestToolSchema_ReviewArgs verifies the atcr_review input schema is inferred
// from the Go struct fields (AC 04-02 Scenario 2).
func TestToolSchema_ReviewArgs(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	tool := listTools(t, cs)[ToolReview]
	require.NotNil(t, tool)
	props := schemaProperties(t, tool)
	for _, f := range []string{"id", "base", "head", "merge_commit"} {
		_, ok := props[f]
		assert.True(t, ok, "atcr_review schema must expose %q", f)
	}
}

// TestToolSchema_ReportFormatEnum verifies the format property carries the
// closed enum so an invalid format is rejected by schema validation (Edge Case 2).
func TestToolSchema_ReportFormatEnum(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	tool := listTools(t, cs)[ToolReport]
	require.NotNil(t, tool)
	props := schemaProperties(t, tool)
	format, ok := props["format"].(map[string]any)
	require.True(t, ok, "format property must be present")
	enum, ok := format["enum"].([]any)
	require.True(t, ok, "format must declare an enum")
	// AC 01-05 (Design Decision #3): FormatAXI is a CLI-subprocess-only format,
	// deliberately filtered OUT of the MCP atcr_report enum — surfacing a
	// token-frugal payload through the token-heavy MCP JSON-RPC envelope defeats its
	// purpose. The enum is the four non-axi formats.
	assert.ElementsMatch(t, []any{"md", "json", "checklist", "sarif"}, enum)
	assert.NotContains(t, enum, "axi", "axi must be excluded from the MCP report enum")
}

// TestMCPReportFormats_AllowListContract pins the atcr_report MCP surface as an
// explicit allow list consulted by BOTH the JSON Schema enum and handleReport's
// defense-in-depth guard (AC 01-05): every advertised format is CLI-valid, the
// CLI-only axi format is absent, and for every format report.FormatList() knows,
// the handler accepts it iff the advertised set contains it — so a future
// CLI-only format added to FormatList() is excluded by construction instead of
// leaking onto the MCP surface through one of the two sites.
func TestMCPReportFormats_AllowListContract(t *testing.T) {
	advertised := mcpReportFormats()
	for _, f := range advertised {
		assert.True(t, report.ValidFormat(f), "MCP-advertised format %q must be CLI-valid", f)
	}
	assert.NotContains(t, advertised, report.FormatAXI, "axi is CLI-only and must stay off the MCP surface")

	e := &engine{root: t.TempDir()}
	for _, f := range report.FormatList() {
		_, _, err := e.handleReport(context.Background(), nil, ReportArgs{Format: f})
		require.Error(t, err, "an empty review root must error after the guard for %q", f)
		if slices.Contains(advertised, f) {
			// Past the guard: it fails later (no review dir), never with the
			// invalid-format error.
			assert.NotContains(t, err.Error(), "invalid format", "advertised format %q must pass the handler guard", f)
		} else {
			assert.Contains(t, err.Error(), "invalid format", "non-advertised CLI format %q must be rejected by the handler guard", f)
		}
	}
}

// schemaProperties extracts the "properties" object from a tool's input schema
// (which arrives client-side as a map[string]any).
func schemaProperties(t *testing.T, tool *mcpsdk.Tool) map[string]any {
	t.Helper()
	schema, ok := tool.InputSchema.(map[string]any)
	require.True(t, ok, "input schema must be a JSON object")
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "input schema must have properties")
	return props
}

func TestToolCall_UnknownTool(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	_, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "atcr_unknown", Arguments: map[string]any{}})
	require.Error(t, err, "calling an unregistered tool must error")
	assert.Contains(t, err.Error(), "atcr_unknown")
}

// TestRegisterTool_Duplicate verifies a duplicate tool name is recorded as an
// error rather than panicking (AC 04-02 Edge Case 1).
func TestRegisterTool_Duplicate(t *testing.T) {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "atcr", Version: Version}, nil)
	r := &registrar{server: s, seen: map[string]bool{}}
	h := func(_ context.Context, _ *mcpsdk.CallToolRequest, _ RangeArgs) (*mcpsdk.CallToolResult, RangeResult, error) {
		return nil, RangeResult{}, nil
	}
	registerTool(r, &mcpsdk.Tool{Name: "dup", Description: "x"}, h)
	require.NoError(t, r.err)
	registerTool(r, &mcpsdk.Tool{Name: "dup", Description: "y"}, h)
	require.Error(t, r.err)
	assert.Contains(t, r.err.Error(), "duplicate")
}

// TestReportInputSchema_Enum verifies the schema builder sets the format enum at
// the source (unit, no transport) and that the enum is derived from the same
// source as report.Formats() so they cannot drift.
func TestReportInputSchema_Enum(t *testing.T) {
	s, err := reportInputSchema()
	require.NoError(t, err)
	require.NotNil(t, s.Properties["format"])
	// The MCP enum is the CLI format list MINUS FormatAXI (AC 01-05, Design
	// Decision #3): axi is a CLI-only format, never surfaced through MCP.
	assert.ElementsMatch(t, []any{"md", "json", "checklist", "sarif"}, s.Properties["format"].Enum)
	assert.NotContains(t, s.Properties["format"].Enum, report.FormatAXI,
		"FormatAXI must be filtered out of the MCP report enum")
}

// TestReportFormatDescriptions_DerivedFromFormats verifies the format list shown
// to users in tool descriptions and schema metadata is derived from the single
// source of truth (report.Formats()), so a future format add/remove cannot drift
// out of sync with the schema enum and report.ValidFormat (AC 04-04).
func TestReportFormatDescriptions_DerivedFromFormats(t *testing.T) {
	s, err := reportInputSchema()
	require.NoError(t, err)
	require.NotNil(t, s.Properties["format"])
	// The description text tracks the MCP-facing (axi-excluded) format list, not the
	// raw report.Formats(), so it stays consistent with the enum (AC 01-05 DoD).
	desc := s.Properties["format"].Description
	assert.NotContains(t, desc, "axi", "the MCP format description must not advertise axi")
	assert.NotContains(t, descReport, "axi", "the atcr_report tool description must not advertise axi")
	for _, f := range []string{"md", "json", "checklist", "sarif"} {
		assert.Contains(t, desc, f, "MCP format description must list %q", f)
		assert.Contains(t, descReport, f, "atcr_report description must list %q", f)
	}
}

// TestRegisterTool_NoOpAfterError verifies that once an error is recorded, later
// registrations are no-ops and do not overwrite the first failure (fail-fast: the
// first error is the one NewServer surfaces).
func TestRegisterTool_NoOpAfterError(t *testing.T) {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "atcr", Version: Version}, nil)
	r := &registrar{server: s, seen: map[string]bool{}}
	h := func(_ context.Context, _ *mcpsdk.CallToolRequest, _ RangeArgs) (*mcpsdk.CallToolResult, RangeResult, error) {
		return nil, RangeResult{}, nil
	}
	registerTool(r, &mcpsdk.Tool{Name: "dup", Description: "x"}, h)
	registerTool(r, &mcpsdk.Tool{Name: "dup", Description: "y"}, h) // records duplicate error
	require.Error(t, r.err)
	first := r.err

	// A subsequent registration of a brand-new tool must be a no-op and leave the
	// first error intact.
	registerTool(r, &mcpsdk.Tool{Name: "fresh", Description: "z"}, h)
	assert.Equal(t, first, r.err, "registration after an error must not overwrite it")
	assert.False(t, r.seen["fresh"], "no-op registration must not mark the tool seen")
}

// schemaUnfriendlyArgs has a field jsonschema inference cannot represent (a
// channel), so mcpsdk.AddTool panics during schema generation. registerTool must
// convert that panic into a recorded error rather than letting it escape.
type schemaUnfriendlyArgs struct {
	Ch chan int `json:"ch"`
}

// TestRegisterTool_RecoversSchemaPanic verifies the deferred recover turns an
// AddTool panic (bad schema) into a fail-fast error naming the tool, so
// NewServer exits cleanly instead of panicking (AC 04-02 Error Scenario 1).
func TestRegisterTool_RecoversSchemaPanic(t *testing.T) {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "atcr", Version: Version}, nil)
	r := &registrar{server: s, seen: map[string]bool{}}
	h := func(_ context.Context, _ *mcpsdk.CallToolRequest, _ schemaUnfriendlyArgs) (*mcpsdk.CallToolResult, RangeResult, error) {
		return nil, RangeResult{}, nil
	}
	require.NotPanics(t, func() {
		registerTool(r, &mcpsdk.Tool{Name: "bad", Description: "x"}, h)
	}, "a schema-generation panic must be recovered, not propagated")
	require.Error(t, r.err)
	assert.Contains(t, r.err.Error(), "bad", "the recorded error must name the failing tool")
}
