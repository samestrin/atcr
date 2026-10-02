package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDocs_ResponseFormatNotHonoredDisclosesTheThinkStrip pins the interim
// caveat in registry.md's `not_honored` cause list (TD docs/registry.md:246).
//
// Doctor's response_format probe checks the RAW reply (its bare-object test
// reads the first byte of the unstripped content), while the review lane strips
// a leading inline `<think>` block before `parseFindings` runs. An endpoint that
// reasons inline and wraps its JSON object in a leading block therefore reads
// as `not_honored` to doctor while the review lane parses the same reply fine —
// an operator who follows the doc's "do not declare it" remedy drops a working
// declaration. Until doctor's probe strips the same block, the cause list must
// disclose this divergence.
func TestDocs_ResponseFormatNotHonoredDisclosesTheThinkStrip(t *testing.T) {
	doc := readDoc(t, "registry.md")
	line := docLineContaining(t, doc, "`not_honored` means the provider rejected the request")

	assert.Contains(t, line, "leading inline `<think>`",
		"the cause list must name the inline-think shape that triggers the false negative")
	assert.Contains(t, line, "the probe checks the RAW reply",
		"the caveat must state the divergence: doctor tests raw content, the review lane parses think-stripped content")
	assert.Contains(t, line, "confirm with the review lane",
		"the remedy must not send the operator straight to dropping the declaration")
}
