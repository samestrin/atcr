package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
	assert.Len(t, listTools(t, cs), 5)
}

func TestToolRegistration_Names(t *testing.T) {
	cs := connectTest(t, t.TempDir(), fakeCompleter{})
	tools := listTools(t, cs)
	for _, want := range []string{ToolReview, ToolReconcile, ToolReport, ToolRange, ToolStatus} {
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
	assert.ElementsMatch(t, []any{"md", "json", "checklist"}, enum)
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
// the source (unit, no transport).
func TestReportInputSchema_Enum(t *testing.T) {
	s, err := reportInputSchema()
	require.NoError(t, err)
	require.NotNil(t, s.Properties["format"])
	assert.ElementsMatch(t, []any{"md", "json", "checklist"}, s.Properties["format"].Enum)
}
