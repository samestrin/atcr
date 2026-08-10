package payload

import (
	"strings"

	"github.com/samestrin/atcr/reconcile"
)

// ScopeFocus renders an agent's per-agent scope categories (Epic 2.2) as a soft
// focus instruction appended to its persona prompt. It is a SOFT constraint:
// it steers the model toward the listed categories but the fan-out never
// hard-drops out-of-category findings, so a genuine cross-cutting issue is not
// silently lost. Blank entries are skipped; an empty/nil scope yields "" so an
// unscoped agent's prompt is unchanged. The leading blank lines separate the
// block from whatever the persona template rendered before it.
func ScopeFocus(scope []string) string {
	cats := make([]string, 0, len(scope))
	for _, c := range scope {
		if c = strings.TrimSpace(c); c != "" {
			cats = append(cats, c)
		}
	}
	if len(cats) == 0 {
		return ""
	}
	// Trust boundary: scope is operator-controlled registry config, validated at
	// load (registry.Load rejects blank/whitespace and control-character entries),
	// so each category is concatenated verbatim with no per-entry escaping. There
	// is intentionally no length bound here — scope is trusted operator input, not
	// attacker-controlled, so a pathological multi-KB entry would only bloat the
	// operator's own prompt. Add a length cap at config.go scope validation if
	// accidental prompt bloat becomes a concern.
	return "\n\n## Review Focus\nConcentrate this review on the following categories: " +
		strings.Join(cats, ", ") + ". Prioritize findings in these areas. This is a focus " +
		"hint, not a hard limit — still report any genuinely critical issue you find outside them. " +
		"These are focus areas, not CATEGORY values: CATEGORY still comes from the closed vocabulary above."
}

// Per-payload-mode scope rules injected into persona prompts as {{.ScopeRule}}.
// diff and blocks constrain findings to the changed regions; files mode widens
// visibility to whole files and routes pre-existing issues to the out-of-scope
// category so the reconciler annotates rather than promotes them.
//
// These are the BASE halves only. Never return one directly from a scope-rule
// accessor: the closed CATEGORY vocabulary is appended in scopeChangedOnlyFull /
// scopeFilesFull below, so returning a base const hands the reviewer a prompt
// with no vocabulary at all (epic 35.16.4).
const (
	scopeChangedOnlyBase = "Review only the changed regions. The payload shows you the change in context, " +
		"but a finding whose FILE:LINE falls outside the changed lines will be discarded before it " +
		"reaches the report when grounding is active (git-range reviews) — it is not enough for the " +
		"code to merely be visible in the surrounding context. If you must flag a genuine pre-existing " +
		"issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than " +
		"discards it. Stay on the diff."

	scopeFilesBase = "Full head-version content of each changed file is provided, so pre-existing issues " +
		"in unchanged regions may be visible. Focus your findings on the changed regions. A finding whose " +
		"FILE:LINE falls outside the changed lines will be discarded before it reaches the report when " +
		"grounding is active (git-range reviews) — it is not enough for the code to merely be visible in " +
		"the surrounding context. You may note a pre-existing issue separately, but give it CATEGORY " +
		"`out-of-scope` so the reconciler annotates rather than promotes it."
)

// categoryEnumeration renders the closed CATEGORY vocabulary
// (reconcile.Categories) as a comma-separated list, in vocabulary order.
//
// It is generated rather than written out so the injected text cannot drift from
// the constant. That is the whole of epic 35.16.4 AC3: the INJECTED enumeration
// has no second literal copy anywhere in the tree.
//
// One literal list does survive, and it is reachable — do not read AC3 as saying
// otherwise. personas/_base.md:44 names six categories in its CATEGORY rule, and
// _base.md is the persona-resolution fallback (internal/registry/persona.go
// levels 4-5) for any rostered agent with no persona file of its own. _base.md
// also carries {{.ScopeRule}}, at line 14 — so an agent resolved that way reads
// this 32-word vocabulary first and the six-word list afterwards, at the position
// of greater recency. Leaving it was a deliberate call recorded in the epic's
// Clarifications, not an oversight; strip the parenthetical from _base.md if that
// ordering ever bites.
func categoryEnumeration() string {
	return strings.Join(mustNonEmptyVocabulary(reconcile.Categories()), ", ")
}

// mustNonEmptyVocabulary returns cats, or panics if the vocabulary is empty.
//
// categoryVocabularyRule is assembled at package init, so an empty vocabulary
// would not fail anywhere near where it could be noticed: every prompt, in every
// mode, would simply ship the sentence "spelled exactly as listed: ." — an
// instruction that names no words — and reviewers would go back to inventing a
// category per finding, which is the exact regression epic 35.16.4 closed.
//
// It refuses rather than falling back to the bare scope rule: a base-only rule is
// the no-vocabulary prompt this epic exists to remove (see the const block
// above), so degrading quietly would swap a visibly broken prompt for an
// invisibly broken one. Reaching here means the pinned reconcile module is
// defective, which is a build-time fault — the same contract regexp.MustCompile
// applies to a bad pattern at init, and it surfaces in CI on the bump that caused
// it rather than in a review transcript weeks later.
func mustNonEmptyVocabulary(cats []string) []string {
	if len(cats) == 0 {
		panic("payload: reconcile.Categories() is empty — every rendered prompt would instruct reviewers to " +
			"spell CATEGORY from an empty list; refusing to build a scope rule with no vocabulary")
	}
	return cats
}

// categoryVocabularyRule is the domain constraint appended to every scope rule.
//
// Epic 35.16.4: before this, no enumeration reached any of the 29 rendered
// prompts — each persona asked only for "one lowercase word", and reviewers
// invented one per finding (34 distinct categories over 213 findings in the
// 35.16.2 dry-run, 72.3% of them unscoreable). The rule NARROWS each persona's
// existing format constraint rather than contradicting it: "one lowercase word"
// stays true, this says which words exist.
//
// It rides {{.ScopeRule}} because that field is the one channel present in all
// 29 prompts — 10 in personas/, 14 in personas/community/, and 5 installed
// outside the repo — and is already allowlisted for fetched community templates
// (internal/registry/persona.go:140-149). Injecting here reaches every prompt,
// including the out-of-repo ones CI cannot see, without editing a single prompt
// file, so no prompt file can fall out of sync.
var categoryVocabularyRule = "Use CATEGORY from this closed vocabulary, spelled exactly as listed: " +
	categoryEnumeration() + ". That list is the whole vocabulary and every finding has a home in it, so " +
	"choose the closest member rather than inventing a word; use `" + reconcile.CategoryOther +
	"` only when no member genuinely fits. `" + reconcile.CategoryOutOfScope +
	"` is a routing value rather than a defect class — use it only for a pre-existing issue as described above, " +
	"never as a category for the change under review. " + categoryDisambiguation

// categoryDisambiguation tells the model how to choose between the members that
// sit closest together.
//
// Without it the enumeration is 32 bare words: the distinctions that justify
// keeping `race` separate from `concurrency` live in reconcile/category.go's
// comments, which no reviewer ever sees, so the per-model drift this epic closes
// would simply reappear as drift between the members of each near-pair. Only the
// genuinely confusable pairs are glossed — glossing all 32 would bury the list.
//
// The words come from the reconcile constants, never from literals, so a rename
// in the vocabulary cannot leave a stale word behind in this sentence.
var categoryDisambiguation = "When two members are close, choose by this rule: `" +
	reconcile.CategoryRace + "` for a specific unsynchronized access to specific shared state, `" +
	reconcile.CategoryConcurrency + "` for other synchronization or lifecycle misuse; `" +
	reconcile.CategoryContract + "` when the change violates an existing contract, `" +
	reconcile.CategoryAPIContract + "` when the published interface is itself wrong; `" +
	reconcile.CategoryResourceLeak + "` for an acquired resource that is never released, `" +
	reconcile.CategoryLeak + "` for anything else retained or exposed; `" +
	reconcile.CategoryInputValidation + "` for untrusted input reaching logic unchecked, `" +
	reconcile.CategoryValidation + "` for any other missing or misplaced check; `" +
	reconcile.CategorySecret + "` for an exposed credential, `" +
	reconcile.CategorySecurity + "` for every other vulnerability; `" +
	reconcile.CategoryCorrectness + "` for a wrong result, with `" +
	reconcile.CategoryLogic + "` accepted as its equivalent."

// Full scope rules as rendered into a prompt: the per-mode scope instruction
// plus the shared vocabulary constraint. Both are assembled once at init rather
// than per call — ScopeRule is invoked per agent per payload chunk on the
// fan-out path (internal/fanout/review.go), and the result is identical every
// time.
var (
	scopeChangedOnlyFull = scopeChangedOnlyBase + " " + categoryVocabularyRule
	scopeFilesFull       = scopeFilesBase + " " + categoryVocabularyRule
)

// ScopeRule returns the scope instruction for a payload mode. diff and blocks
// share the changed-only rule (function-context expansion does not widen the
// review scope); files mode uses the wider rule. An unknown mode falls back to
// the conservative changed-only rule. Every mode carries the closed CATEGORY
// vocabulary — an unknown mode must not be a hole through which a reviewer is
// offered no vocabulary at all.
func ScopeRule(mode PayloadMode) string {
	switch mode {
	case ModeFiles:
		return scopeFilesFull
	case ModeDiff, ModeBlocks:
		return scopeChangedOnlyFull
	default:
		return scopeChangedOnlyFull
	}
}

// ScopeRuleForPayload returns the scope instruction for the payload text an
// agent will actually receive, accounting for per-file escalation (Epic 35.1).
//
// A persona prompt carries exactly one {{.ScopeRule}}, but escalation means one
// payload can mix modes. If ANY file in this payload was rendered as a full
// file, its unchanged regions are in front of the reviewer, so the payload gets
// the wider files-mode rule. Telling a reviewer to stay on the diff while
// handing it whole files is the failure mode worth avoiding; the reverse (the
// wider rule over a mostly-diff payload) merely permits findings the grounding
// gate already filters.
//
// The decision is made from the text rather than from []FileEntry because the
// chunked review path splits a payload into text chunks before rendering, and a
// chunk that contains no escalated file should keep the narrow rule. Detection
// keys on a files-mode header at the start of a line: inside a unified diff
// every content line carries a +/-/space prefix, so a header can never appear at
// column 0 unless a file really was rendered in files mode.
func ScopeRuleForPayload(mode PayloadMode, text string) string {
	if containsFilesHeader(text) {
		return scopeFilesFull
	}
	return ScopeRule(mode)
}

// containsFilesHeader reports whether text has a files-mode header at the start
// of any line.
func containsFilesHeader(text string) bool {
	if strings.HasPrefix(text, filesHeaderPrefix) {
		return true
	}
	return strings.Contains(text, "\n"+filesHeaderPrefix)
}
