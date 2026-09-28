package fanout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/llmclient"
)

// TestInvokeSlot_LabeledJSONFenceCleanReply_IsClean pins the engine-side half
// of TD internal/fanout/engine.go:903: a clean reply fenced under a labeled
// opener ("```json response") must not be recorded UnparseableResponse. The
// fence scanner parses it as an empty array (zero findings); IsNoFindings must
// agree, so the gate that flags non-sentinel zero-finding content stays
// silent — the reviewer keeps its trust credit for a review it actually
// completed.
func TestInvokeSlot_LabeledJSONFenceCleanReply_IsClean(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "```json response\n[]\n```"},
	}}
	e := NewEngine(c)
	slot := Slot{Primary: Agent{Name: "brad", Invocation: llmclient.Invocation{Model: "primary"}}}
	r := e.invokeSlot(context.Background(), slot)

	assert.Equal(t, StatusOK, r.Status, "a clean review is a legitimate outcome, not a failure")
	assert.False(t, r.UnparseableResponse,
		"the scanner parsed the fenced empty array, so this is a clean review, not garbage")
	assert.Equal(t, 0, r.ParsedFindingCount(), "and it yields no findings")
}
