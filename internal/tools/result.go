package tools

// ToolResult is the bounded output of a single tool invocation. The agent loop
// delivers Content to the model as the body of a role:"tool" message.
//
// Truncated reports whether Content was shortened to fit a cap; OriginalBytes
// records the size (in bytes, or match count for grep) before truncation so the
// transcript and status counters can reflect what the tool actually produced.
type ToolResult struct {
	Content       string
	Truncated     bool
	OriginalBytes int
}
