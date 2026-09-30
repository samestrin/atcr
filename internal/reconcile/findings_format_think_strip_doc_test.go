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

	// T6, same sprint. The salvage refusal vanishes strictly MORE reviewer output
	// than the think strip does (a whole reply, and for a chunked review a whole
	// bin), so the same "operator whose output vanished reads this doc" argument
	// applies with more force. The per-chunk half is pinned too: refusing the whole
	// persona instead would contradict the chunk contract in the paragraph below.
	t.Run("the salvage refusal is named, and it is per chunk", func(t *testing.T) {
		for _, want := range []string{
			"salvaged from the\nmodel's reasoning",
			"yields no findings at all",
			"a draft the model abandoned",
			"that refusal is per chunk",
			"sibling chunks' findings are kept",
		} {
			flat := strings.ReplaceAll(want, "\n", " ")
			assert.True(t, strings.Contains(doc, want) || strings.Contains(doc, flat),
				"findings-format.md must state the salvage refusal: missing %q", flat)
		}
	})

	// The chunk contract the per-chunk refusal exists to keep true. If a future
	// change goes back to refusing on the persona-wide Salvaged bit, a salvaged bin
	// beside a bin with findings WOULD mark the persona unparseable and this
	// sentence would become false.
	t.Run("the chunk contract still holds", func(t *testing.T) {
		for _, want := range []string{
			"one garbled chunk beside a chunk with findings",
			"without marking the persona unparseable",
		} {
			assert.Contains(t, doc, want,
				"findings-format.md's chunk contract must hold: missing %q", want)
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
