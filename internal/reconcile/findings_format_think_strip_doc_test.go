package reconcile

import (
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
// Tokens, not whole sentences: the doc is one long line per paragraph — these
// docs are never hard-wrapped, so every want below is a single-line literal and
// no assertion applies wrap tolerance. A reworded connective must not fail a
// test whose subject is the claim.
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
			"salvaged from the model's reasoning",
			"yields no findings at all",
			"a draft the model abandoned",
			"that refusal is per chunk",
			"sibling chunks' findings are kept",
			// Where the refused bin is COUNTED depends on why it salvaged, and the
			// doc has to keep both halves: a stop-reason salvage stays ok and is
			// unparseable, a length-cutoff one fails over and is unreviewed.
			"stop-reason salvage stays `ok`",
			"counted in `unparseable_chunks`",
			"fails over to the backup model",
			"counted in `unreviewed_chunks`",
		} {
			assert.Contains(t, doc, want,
				"findings-format.md must state the salvage refusal: missing %q", want)
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
			"reads the raw `review.md`",
			"the elision and the parser can disagree",
		} {
			assert.Contains(t, doc, want,
				"findings-format.md must disclose the excerpt-vs-parser drift: missing %q", want)
		}
	})
}

// docs/providers.md is the operator-facing provider guide, and its
// reasoning-normalization bullet described the salvage's ORIGINAL purpose: that
// atcr falls back to reasoning_content to extract findings from. Sprint
// 35.16.11.2.2.4 T6 reversed that (internal/fanout/engine.go:604-605 returns no
// findings for a salvaged reply; internal/llmclient/client.go:413-418 records the
// reversal in-source), so the bullet promised behavior the code had stopped
// doing. The sprint never touched this file, which is exactly why it needs a
// guard: the drift is invisible to a reviewer reading only the diff.
//
// Tokens, not whole sentences: the doc is one long line per bullet, but a
// reworded connective must not fail a test whose subject is the claim.
func TestProvidersDoc_SalvageYieldsNoFindings(t *testing.T) {
	doc := readDoc(t, "providers.md")

	t.Run("the salvage is named as diagnostic-only", func(t *testing.T) {
		for _, want := range []string{
			"marks the reply **salvaged**",
			"for diagnosability only",
			// The two places a salvaged reply still shows up. If a later edit drops
			// these, the salvage reads as pointless and invites removal.
			//
			// Exactly two, not three: the transcript is NOT one of them. Only the
			// tool loop writes a transcript (internal/fanout/loop.go), and
			// llmclient.ChatResponse carries no Salvaged field, so a salvaged reply
			// is always a single-shot reply. The Phase 5 gate caught this doc
			// claiming the transcript; asserting the two-place list keeps the third
			// from being added back.
			"`review.md` and `atcr doctor`'s hint",
		} {
			assert.Contains(t, doc, want,
				"providers.md must describe the salvage as diagnostic-only: missing %q", want)
		}
	})

	t.Run("the refusal is stated for every lane", func(t *testing.T) {
		for _, want := range []string{
			"yields no findings, verdict, ruling, or cache entry",
			"every lane refuses it",
			"worse than no answer",
		} {
			assert.Contains(t, doc, want,
				"providers.md must state that a salvaged reply is refused: missing %q", want)
		}
	})

	// The strip belongs in the same bullet because it is the other half of "what
	// atcr does to content before parsing it", and the qualifier is load-bearing:
	// SplitThink is leading-only, so an unqualified claim here would repeat the
	// overclaim the Phase 5 gate caught twice in docs/registry.md.
	t.Run("the think strip is named and qualified as leading-only", func(t *testing.T) {
		for _, want := range []string{
			"A **leading** inline `<think>…</think>` run is stripped from `content` before parsing",
			"leading-only: a block after answer text is left in place",
		} {
			assert.Contains(t, doc, want,
				"providers.md must state the leading-only think strip: missing %q", want)
		}
	})
}
