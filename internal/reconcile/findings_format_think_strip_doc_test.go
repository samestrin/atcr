package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
)

// Paragraph markers for findings-format.md. Every assertion below is scoped to
// the ONE line carrying the paragraph its subtest names, because a phrase that
// matches anywhere in a 343-line document is not a guard: a sentence relocated
// to an unrelated section satisfied the whole-document form of these assertions
// just as well as one that stayed put.
const (
	ffParseContractMarker  = "Reviewer models do not write v2 files."
	ffFenceGrammarMarker   = "Anything inside another code fence is a quoted example"
	ffDroppedFindingMarker = "An object with an unknown severity or no location is dropped."
	ffJustificationMarker  = "- `justification` — the narrative section extracted"
	// providers.md is a bullet-per-paragraph file, so the bullet IS the paragraph.
	provNormalizationMarker = "**Reasoning/thinking token normalization.**"
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
//
// Scoping is per paragraph (docLineContaining), and each subtest also carries an
// operand anchored to the CODE. A doc-only guard is revert-blind: T4 (the strip)
// and T6 (the salvage refusal) can each be reverted with no doc edit and leave a
// suite that runs green while the doc describes behavior the code no longer has.
func TestFindingsFormatDoc_StatesTheThinkStrip(t *testing.T) {
	doc := readDoc(t, "findings-format.md")

	t.Run("the strip is named in the parsing contract", func(t *testing.T) {
		line := docLineContaining(t, doc, ffParseContractMarker)
		for _, want := range []string{
			"inline `<think>...</think>` reasoning block",
			"removed before it is parsed",
			"before the `NO FINDINGS` check",
			"leading-only",
			"`review.md` always holds the raw reply",
		} {
			assert.Contains(t, line, want,
				"findings-format.md must state the think strip: missing %q", want)
		}

		// Code anchor. T4's strip IS llmclient.SplitThink, so the doc's
		// "leading-only" qualifier is checkable rather than folklore: the leading
		// run goes, and a run after answer text does not. A revert of the strip
		// moves both results — the half a doc-only guard cannot see.
		answer, reasoning := llmclient.SplitThink("<think>draft</think>Real answer.")
		assert.Equal(t, "Real answer.", answer,
			"the leading think run is what T4's strip removes; a doc claiming so must fail when the strip stops removing it")
		assert.Equal(t, llmclient.Reasoning("draft"), reasoning,
			"the removed run is returned as reasoning, not dropped on the floor")

		nonLeading, nonLeadingRemoved := llmclient.SplitThink("Real answer. <think>quoted</think>")
		assert.Equal(t, "Real answer. <think>quoted</think>", nonLeading,
			"leading-ONLY: a think run after answer text is answer text, which is the claim the doc's qualifier makes")
		assert.Empty(t, nonLeadingRemoved,
			"a non-leading run is not moved into reasoning either, so the qualifier is not merely a doc typo")
	})

	t.Run("the accepted unclosed-opener loss is named", func(t *testing.T) {
		line := docLineContaining(t, doc, ffFenceGrammarMarker)
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
			assert.Contains(t, line, want,
				"findings-format.md must state the unclosed-opener loss: missing %q", want)
		}

		// Code anchor, on the cut-off half — the half whose behavior is stable and
		// externally observable. "Read as reasoning end to end" is exactly this:
		// the answer is empty and the whole remainder is the removed reasoning. A
		// revert of the unclosed-opener branch (strip nothing instead) makes answer
		// non-empty and fails here.
		cutOff, cutOffReasoning := llmclient.SplitThink("<think>cut off mid-thought")
		assert.Empty(t, cutOff,
			"a reply cut off inside an unclosed opener has no answer text once the run is taken as reasoning")
		assert.Equal(t, llmclient.Reasoning("cut off mid-thought"), cutOffReasoning,
			"everything after the unclosed opener is reasoning, which is what makes the doc's loss real")
	})

	// T6, same sprint. The salvage refusal vanishes strictly MORE reviewer output
	// than the think strip does (a whole reply, and for a chunked review a whole
	// bin), so the same "operator whose output vanished reads this doc" argument
	// applies with more force. The per-chunk half is pinned too: refusing the whole
	// persona instead would contradict the chunk contract in the paragraph below.
	t.Run("the salvage refusal is named, and it is per chunk", func(t *testing.T) {
		line := docLineContaining(t, doc, ffParseContractMarker)
		for _, want := range []string{
			"salvaged from the model's reasoning",
			"yields no findings at all",
			"a draft the model abandoned",
			"that refusal is per chunk",
			"sibling chunks' findings are kept",
			// The chunked path reads each bin's own stop reason (chunkSalvagedOnStop),
			// not the persona-wide truncation fold, so a stop-reason bin is parsed
			// and only a bin with no stop reason is refused. Pinned in-package by
			// TestMergeResultGroup_StopReasonSalvagedChunkKeepsItsFindings.
			"a bin that salvaged on a stop reason is parsed like any other chunk",
			"a salvaged bin with no stop reason recorded is refused",
			// Where a salvaged bin is COUNTED depends on why it salvaged, and the
			// doc has to keep both halves: a stop-reason salvage stays ok and is
			// scored like any chunk, a length-cutoff one fails over and is unreviewed.
			"stop-reason salvage stays `ok`",
			"counted in `unparseable_chunks` only when its answer is neither findings nor a clean review",
			"fails over to the backup model",
			"counted in `unreviewed_chunks`",
		} {
			assert.Contains(t, line, want,
				"findings-format.md must state the salvage refusal: missing %q", want)
		}

		// Code anchor on T6's refusal itself. "Yields no findings at all" is a
		// claim about ParsedFindingCount, so assert it against a salvaged Result
		// whose content is a perfectly parseable findings block: a revert of the
		// refusal parses it and this fails. The CHUNKED half of the claim has no
		// external constructor (chunkContents/chunkSalvaged are unexported and
		// mergeResultGroup is package-private), so it stays doc-only above and is
		// pinned in-package by
		// TestMergeResultGroup_SalvagedLaterChunkKeepsSiblingFindings.
		// Unfenced on purpose: docs/findings-format.md records that an unfenced
		// JSON array in plain prose is READ as findings, so this is the documented
		// shape and it keeps the literal on one line
		// (TestFindingsFormatThinkStripDocTest_OneWrapPolicy).
		findingsBlock := `[{"severity":"LOW","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":5,"evidence":"e"}]`
		// ResponseTruncated is part of the fixture because the doc sentence it anchors
		// is "yields no findings at all WHEN the provider ALSO stopped that reply on
		// length" (TD internal/fanout/engine.go:604). The assertion is unchanged; the
		// shape that earns it is now stated in full.
		salvaged := &fanout.Result{Salvaged: true, ResponseTruncated: true, Content: findingsBlock}
		assert.Equal(t, 0, salvaged.ParsedFindingCount(),
			"a salvaged reply cut off on length yields no findings even when its content would parse, which is precisely T6's reversal")
		// The other half of the same doc sentence, anchored so the stop-reason carve-out
		// cannot drift either: a salvage the provider did NOT cut off is a finished
		// answer on the reasoning channel and must parse.
		salvagedOnStop := &fanout.Result{Salvaged: true, Content: findingsBlock}
		assert.Equal(t, 1, salvagedOnStop.ParsedFindingCount(),
			"a stop-reason salvage is a completed answer on the reasoning channel, so the doc's carve-out must hold in code")
		unsalvaged := &fanout.Result{Content: findingsBlock}
		assert.Equal(t, 1, unsalvaged.ParsedFindingCount(),
			"the control: the same content unsalvaged DOES parse, so the zero above is the refusal and not a malformed fixture")
	})

	// The chunk contract the per-chunk refusal exists to keep true. If a future
	// change goes back to refusing on the persona-wide Salvaged bit, a salvaged bin
	// beside a bin with findings WOULD mark the persona unparseable and this
	// sentence would become false.
	t.Run("the chunk contract still holds", func(t *testing.T) {
		line := docLineContaining(t, doc, ffDroppedFindingMarker)
		for _, want := range []string{
			"one garbled chunk beside a chunk with findings",
			"without marking the persona unparseable",
		} {
			assert.Contains(t, line, want,
				"findings-format.md's chunk contract must hold: missing %q", want)
		}
	})

	// Epic 35.16.11.2.2.4.5.1 T7. The salvage sentence in the chunk-contract
	// paragraph used to say every salvaged reply was "refused rather than parsed".
	// Slice 1 made the findings lane parse a stop-reason salvage and score its
	// committed NO FINDINGS clean, and T3 indexed its narratives, so the sentence now
	// names the abandoned-only refusal. Each clause carries a code anchor: a revert
	// of any one of the three behaviors fails here even with the doc untouched.
	t.Run("only an abandoned salvage is refused by the findings lane", func(t *testing.T) {
		line := docLineContaining(t, doc, ffDroppedFindingMarker)
		for _, want := range []string{
			"Only an abandoned salvage is refused rather than parsed",
			"one the provider cut off on length",
			"A stop-reason salvage is the model's finished answer arriving on the reasoning channel",
			"its findings are parsed",
			"its narratives are indexed for `justification`",
			"a committed `NO FINDINGS` on that channel is a clean review rather than `unparseable_response`",
			"prose on that channel that yields no findings is still `unparseable_response`",
			// The other lanes' refusal is unchanged (T1, T2) and must stay stated, so
			// the findings-lane carve-out is not read as a repo-wide one.
			"the verify, executor and debate lanes refuse a salvage of either kind",
			"no salvage is ever cached",
		} {
			assert.Contains(t, line, want,
				"findings-format.md must state the abandoned-only refusal: missing %q", want)
		}

		// Code anchor, "its findings are parsed": the same parseable block yields a
		// finding on a stop-reason salvage and nothing on a length-cutoff one.
		findingsBlock := `[{"severity":"LOW","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":5,"evidence":"e"}]`
		assert.Equal(t, 1, (&fanout.Result{Salvaged: true, SalvagedOnStop: true, Content: findingsBlock}).ParsedFindingCount(),
			"a stop-reason salvage's findings are parsed")
		assert.Equal(t, 0, (&fanout.Result{Salvaged: true, ResponseTruncated: true, Content: findingsBlock}).ParsedFindingCount(),
			"an abandoned salvage is refused")

		// Code anchor, "a committed NO FINDINGS on that channel is a clean review":
		// run a real slot so invokeSlot's sentinel arm decides, for the two salvage
		// classes and for prose on the reasoning channel.
		run := func(comp llmclient.Completion) fanout.Result {
			e := fanout.NewEngine(salvageCompleter{comp: comp})
			res := e.Run(context.Background(), []fanout.Slot{{Primary: fanout.Agent{Name: "r", Invocation: llmclient.Invocation{Model: "m"}}}})
			require.Len(t, res, 1)
			return res[0]
		}
		clean := run(llmclient.Completion{Content: "NO FINDINGS", Salvaged: true, SalvagedOnStop: true})
		assert.Equal(t, fanout.StatusOK, clean.Status)
		assert.False(t, clean.UnparseableResponse,
			"a committed NO FINDINGS on the reasoning channel is a clean review")
		prose := run(llmclient.Completion{Content: "I looked around.", Salvaged: true, SalvagedOnStop: true})
		assert.True(t, prose.UnparseableResponse,
			"prose on the reasoning channel that yields no findings is still unparseable")
		cutOff := run(llmclient.Completion{Content: "NO FINDINGS", Salvaged: true, SalvagedTruncated: true, Truncated: true})
		assert.True(t, cutOff.UnparseableResponse,
			"a sentinel-shaped draft the provider cut off is not a committed clean review")

		// Code anchor, "its narratives are indexed": sourceSalvage is the reader
		// collectReviewNarratives withholds on, and it reports a stop-reason salvage
		// as not salvaged. The full stamping path is pinned by
		// TestStampJustifications_StopReasonSalvageContributesItsNarrative.
		salvaged, bins := sourceSalvageOf(t, `{"salvaged":true,"salvaged_on_stop":true}`)
		assert.False(t, salvaged, "a stop-reason salvage's narrative is indexed, not withheld")
		assert.Empty(t, bins)
	})

	t.Run("the justification excerpt drift is disclosed", func(t *testing.T) {
		line := docLineContaining(t, doc, ffJustificationMarker)
		for _, want := range []string{
			"reads the raw `review.md`",
			"the elision and the parser can disagree",
		} {
			assert.Contains(t, line, want,
				"findings-format.md must disclose the excerpt-vs-parser drift: missing %q", want)
		}
	})

	// Epic 35.16.11.2.2.4.5.1 T7 (code: T3). The justification paragraph said the
	// producer refuses any salvaged unchunked reply whole; T3 made reconcile withhold
	// only an ABANDONED salvage's narrative and index a stop-reason one's.
	t.Run("only an abandoned salvage's narrative is withheld", func(t *testing.T) {
		line := docLineContaining(t, doc, ffJustificationMarker)
		for _, want := range []string{
			"an abandoned salvaged bin's lines are skipped whole",
			"a bin listed in `salvaged_on_stop_chunks` is indexed",
			"`status.json` records an abandoned salvage",
			"`salvaged` with no `salvaged_chunks` and no `salvaged_on_stop`",
			"A stop-reason salvage is not omitted: its narratives are indexed like any other reply's",
		} {
			assert.Contains(t, line, want,
				"findings-format.md must state which salvaged narratives are withheld: missing %q", want)
		}

		// Code anchor: the three shapes the sentence names, read by the same
		// function collectReviewNarratives consults.
		salvaged, bins := sourceSalvageOf(t, `{"salvaged":true}`)
		assert.True(t, salvaged, "an unchunked salvage with no stop reason is abandoned and withheld whole")
		assert.Empty(t, bins, "no bin index: the whole file is withheld")

		salvaged, bins = sourceSalvageOf(t, `{"salvaged":true,"salvaged_on_stop":true}`)
		assert.False(t, salvaged, "a stop-reason salvage is not omitted")
		assert.Empty(t, bins)

		salvaged, bins = sourceSalvageOf(t, `{"salvaged":true,"salvaged_chunks":[1,2],"salvaged_on_stop_chunks":[1]}`)
		assert.True(t, salvaged)
		assert.Equal(t, []int{2}, bins, "only the abandoned bin's lines are skipped; the on-stop bin is indexed")
	})
}

// sourceSalvageOf writes status beside a review.md in a fresh dir and returns
// what sourceSalvage reads from it.
func sourceSalvageOf(t *testing.T, status string) (bool, []int) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, statusFileName), []byte(status), 0o600))
	return sourceSalvage(filepath.Join(dir, "review.md"))
}

// salvageCompleter returns one fixed Completion through the MetaCompleter path,
// the only path that carries the salvage reason.
type salvageCompleter struct{ comp llmclient.Completion }

func (s salvageCompleter) Complete(context.Context, llmclient.Invocation) (string, error) {
	return s.comp.Content, nil
}

func (s salvageCompleter) CompleteWithMeta(context.Context, llmclient.Invocation) (llmclient.Completion, error) {
	return s.comp, nil
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
		line := docLineContaining(t, doc, provNormalizationMarker)
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
			assert.Contains(t, line, want,
				"providers.md must describe the salvage as diagnostic-only: missing %q", want)
		}
	})

	// Epic 35.16.11.2.2.4.5.1 T7. This bullet said "every lane refuses it" and that a
	// salvaged reply "yields no findings, verdict, ruling, or cache entry". Slice 1
	// made the findings lane parse a stop-reason salvage, so the findings half is now
	// abandoned-only, while verify, the executor and debate (T1, T2) and the diff
	// cache still refuse both kinds — and the bullet must keep saying so.
	t.Run("the findings lane refuses only an abandoned salvage; every other lane refuses both", func(t *testing.T) {
		line := docLineContaining(t, doc, provNormalizationMarker)
		for _, want := range []string{
			"the findings lane refuses only an **abandoned** salvage, one the provider cut off on length",
			"worse than no answer",
			"A salvage on a stop reason is a finished answer",
			"the findings lane parses its findings",
			"a committed `NO FINDINGS` there is a clean review",
			"Every other lane refuses both kinds",
			"yields no verdict, ruling, executor patch, or cache entry",
		} {
			assert.Contains(t, line, want,
				"providers.md must state which lanes refuse a salvaged reply: missing %q", want)
		}
		assert.NotContains(t, line, "every lane refuses it",
			"the blanket claim is false for the findings lane since slice 1")

		// Code anchor on the findings-lane half: the same block parses on a
		// stop-reason salvage and is refused on a length-cutoff one. The other
		// lanes' refusal is pinned in their own packages
		// (TestInvokeSkeptic_StopReasonSalvageIsRefused,
		// TestGenerateFixes_StopReasonSalvage_NoPatchAndLogsSalvageClass,
		// TestRunDebate_SalvagedStopReasonSeatHaltsAndIsNotForwarded), which this
		// package cannot import without a cycle.
		findingsBlock := `[{"severity":"LOW","file_line":"a.go:1","problem":"p","fix":"f","category":"c","est_minutes":5,"evidence":"e"}]`
		assert.Equal(t, 1, (&fanout.Result{Salvaged: true, SalvagedOnStop: true, Content: findingsBlock}).ParsedFindingCount(),
			"the findings lane parses a stop-reason salvage")
		assert.Equal(t, 0, (&fanout.Result{Salvaged: true, ResponseTruncated: true, Content: findingsBlock}).ParsedFindingCount(),
			"the findings lane refuses an abandoned salvage")
	})

	// The strip belongs in the same bullet because it is the other half of "what
	// atcr does to content before parsing it", and the qualifier is load-bearing:
	// SplitThink is leading-only, so an unqualified claim here would repeat the
	// overclaim the Phase 5 gate caught twice in docs/registry.md.
	t.Run("the think strip is named and qualified as leading-only", func(t *testing.T) {
		line := docLineContaining(t, doc, provNormalizationMarker)
		for _, want := range []string{
			"A **leading** inline `<think>…</think>` run is stripped from `content` before parsing",
			"leading-only: a block after answer text is left in place",
		} {
			assert.Contains(t, line, want,
				"providers.md must state the leading-only think strip: missing %q", want)
		}

		// Code anchor. The bullet's two halves name the strip's actual behavior,
		// so assert them: a leading run leaves `content` before parsing, and a
		// block after answer text does not. Deliberately the same clause the
		// findings-format guard asserts — this repo asserts a shared clause in
		// BOTH documents precisely so the pair cannot drift apart (see
		// skeptic_budget_docs_test.go).
		stripped, removed := llmclient.SplitThink("<think>reasoning</think>Answer body.")
		assert.Equal(t, "Answer body.", stripped,
			"a leading run is stripped from the content before parsing, which is what this bullet promises")
		assert.Equal(t, llmclient.Reasoning("reasoning"), removed,
			"the strip is not a no-op: it removes the run rather than passing the reply through")
		kept, _ := llmclient.SplitThink("Answer body. <think>a quoted tag</think>")
		assert.Equal(t, "Answer body. <think>a quoted tag</think>", kept,
			"leading-ONLY is the qualifier the bullet carries; a run after answer text stays put")
	})
}
