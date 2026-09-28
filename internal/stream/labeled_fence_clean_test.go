package stream

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsNoFindings_LabeledJSONFenceClean pins the contract decided by TD
// internal/fanout/engine.go:903: a fenced clean reply whose opener carries a
// second bare word — "```json response" — is the same output the fence scanner
// already reads as a ```json block (zero findings, no error), so IsNoFindings
// must agree and call it clean. Before this pin, isInfoString rejected the
// second bare word, the opener stayed content, and a legitimate clean review
// was recorded UnparseableResponse with no trust credit while the scanner
// parsed it fine — parser and IsNoFindings disagreed on one reply.
//
// A second bare word is accepted only when the opener's rest carries none of
// the content-like characters (pipe, colon, dot), so the shapes those protect —
// a pipe row sharing the marker line ("```HIGH|a.go:1|...") and referenced
// prose ("```see a.go:3") — stay content, and a reply holding them is NOT
// called clean.
func TestIsNoFindings_LabeledJSONFenceClean(t *testing.T) {
	clean := []string{
		"```json response\n[]\n```",
		"```json response\n{\"findings\":[]}\n```",
		"```json output\n[]\n```",
	}
	for _, c := range clean {
		assert.True(t, IsNoFindings(c), "labeled-fence clean reply must read clean: %q", c)
		assert.Equal(t, 0, len(ParseModelOutput([]byte(c))),
			"the scanner must read the same reply as zero findings: %q", c)
	}

	notClean := []string{
		// A pipe row shares the marker line: that is content, not metadata.
		"```HIGH|a.go:1|bug|fix|correctness|5|ev|rev\n[]\n```",
		// Referenced prose after the marker is content.
		"```see a.go:3 for the nil deref\n[]\n```",
		// A bare word followed by dotted prose is content.
		"```json response.md stays prose\n[]\n```",
	}
	for _, c := range notClean {
		assert.False(t, IsNoFindings(c), "content-like opener must not read clean: %q", c)
	}
}
