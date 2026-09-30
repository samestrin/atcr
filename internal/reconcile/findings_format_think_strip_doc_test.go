package reconcile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The findings lane strips a leading <think> block before parsing (sprint
// 35.16.11.2.2.4 T4). docs/findings-format.md is the published parsing contract
// for that lane, so an operator whose reviewer output "vanished" reads it to find
// out why. Both sibling lanes' docs were pinned the same way this sprint
// (skeptic_budget_docs_test.go), and CLAUDE.md requires a doc-vs-code drift test
// to live in internal/reconcile/.
//
// Tokens, not whole sentences: the doc is one long line per paragraph, but a
// reworded connective must not fail a test whose subject is the claim.
func TestFindingsFormatDoc_StatesTheThinkStrip(t *testing.T) {
	doc := readDoc(t, "findings-format.md")

	t.Run("the strip is named in the parsing contract", func(t *testing.T) {
		for _, want := range []string{
			"inline `<think>...</think>` reasoning block",
			"removed before it is parsed",
			"before the `NO FINDINGS` check",
			"leading-only",
			"`review.md` always holds the raw reply",
		} {
			assert.Contains(t, doc, want,
				"findings-format.md must state the think strip: missing %q", want)
		}
	})

	t.Run("the accepted unclosed-opener loss is named", func(t *testing.T) {
		for _, want := range []string{
			"opens with `<think>` and never closes it",
			"`</thinking>`",
			// The two ways a closer goes missing land differently, and the doc
			// must keep saying so: a complete reply with a variant closer is
			// recorded unparseable, a cut-off one fails over instead.
			"any finding after the block is lost",
			"recorded `unparseable_response`",
			"failed over to its backup model",
		} {
			assert.Contains(t, doc, want,
				"findings-format.md must state the unclosed-opener loss: missing %q", want)
		}
	})

	t.Run("the justification excerpt drift is disclosed", func(t *testing.T) {
		for _, want := range []string{
			"reads the raw\n`review.md`",
			"the elision\nand the parser can disagree",
		} {
			flat := strings.ReplaceAll(want, "\n", " ")
			assert.True(t, strings.Contains(doc, want) || strings.Contains(doc, flat),
				"findings-format.md must disclose the excerpt-vs-parser drift: missing %q", flat)
		}
	})
}
