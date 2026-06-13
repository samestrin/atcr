package fanout

import (
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
)

func TestToolSig_CanonicalizesKeyOrder(t *testing.T) {
	a := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage(`{"b":2,"a":1}`)}}
	b := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage(`{"a":1,"b":2}`)}}
	assert.Equal(t, toolSig(a), toolSig(b), "semantically identical object arguments with reordered keys must produce the same signature")
}

func TestToolSig_CanonicalizesWhitespace(t *testing.T) {
	a := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage(`{"a":1,"b":2}`)}}
	b := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: json.RawMessage("{\"a\": 1,\n \"b\": 2}")}}
	assert.Equal(t, toolSig(a), toolSig(b), "semantically identical arguments with differing whitespace must produce the same signature")
}

func TestToolSig_FallsBackForInvalidJSON(t *testing.T) {
	raw := json.RawMessage(`not json`)
	a := llmclient.ToolCall{Function: llmclient.FunctionCall{Name: "read", Arguments: raw}}
	assert.Equal(t, "read\x00not json", toolSig(a), "invalid JSON args must fall back to raw bytes")
}
