package verify

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// argDispatcher records the raw args of every read_file call.
type argDispatcher struct {
	mu   sync.Mutex
	args []json.RawMessage
}

func (d *argDispatcher) Execute(_ context.Context, _ string, args json.RawMessage) (tools.ToolResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.args = append(d.args, args)
	return tools.ToolResult{Content: "snippet"}, nil
}

func TestFixTarget(t *testing.T) {
	cases := []struct {
		name string
		f    reconcile.JSONFinding
		file string
		line int
	}{
		{"no marker", reconcile.JSONFinding{File: "a.go", Line: 3}, "a.go", 3},
		{"marker", reconcile.JSONFinding{File: "a.go", Line: 3, SiblingLocation: "b/c.go:42"}, "b/c.go", 42},
		{"no line suffix", reconcile.JSONFinding{File: "a.go", Line: 3, SiblingLocation: "b/c.go"}, "b/c.go", 0},
		{"non-numeric suffix", reconcile.JSONFinding{File: "a.go", Line: 3, SiblingLocation: "b/c.go:x"}, "b/c.go:x", 0},
		{"trailing colon", reconcile.JSONFinding{File: "a.go", Line: 3, SiblingLocation: "b/c.go:"}, "b/c.go", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file, line := fixTarget(c.f)
			assert.Equal(t, c.file, file)
			assert.Equal(t, c.line, line)
		})
	}
}

func TestGenerateFixes_SnippetReadAtSiblingLocation(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
			SiblingLocation: "sib/b.go:100"},
	}
	rec := &recordingExecutor{out: "fix"}
	disp := &argDispatcher{}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, disp, 0)

	require.Len(t, disp.args, 1)
	var got struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
	}
	require.NoError(t, json.Unmarshal(disp.args[0], &got))
	assert.Equal(t, "sib/b.go", got.Path)
	assert.Equal(t, 100-fixSnippetRadius, got.StartLine)
}

func TestGenerateFixes_SnippetReadWithoutMarkerUnchanged(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified},
	}
	rec := &recordingExecutor{out: "fix"}
	disp := &argDispatcher{}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, disp, 0)

	require.Len(t, disp.args, 1)
	var got struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(disp.args[0], &got))
	assert.Equal(t, "a.go", got.Path)
}

func TestBuildFixPrompt_NamesSiblingLocation(t *testing.T) {
	f := reconcile.JSONFinding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", SiblingLocation: "sib/b.go:100"}
	p := buildFixPrompt(f, "100: x", execConfig("MEDIUM"), "")
	assert.Contains(t, p, "Location: sib/b.go:100")
	assert.Contains(t, p, "Source context around sib/b.go:100")
	assert.NotContains(t, p, "a.go:1")

	f.SiblingLocation = ""
	p = buildFixPrompt(f, "1: x", execConfig("MEDIUM"), "")
	assert.Contains(t, p, "Location: a.go:1")
	assert.Contains(t, p, "Source context around a.go:1")
}

func TestBuildExecutorAgentPrompt_NamesSiblingLocation(t *testing.T) {
	f := reconcile.JSONFinding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", SiblingLocation: "sib/b.go:100"}
	p := buildExecutorAgentPrompt(f, "")
	assert.Contains(t, p, "sib/b.go:100")
	assert.NotContains(t, p, "a.go:1")

	f.SiblingLocation = ""
	assert.Contains(t, buildExecutorAgentPrompt(f, ""), "a.go:1")
}
