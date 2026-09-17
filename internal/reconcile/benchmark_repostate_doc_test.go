package reconcile

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/benchmark.md's "Running a `repo-state-v1` suite" section documents the second
// benchmark tier: where its metric lives, what its JSON keys are called, and which
// standard-v1 behaviours it deliberately does NOT share. Nothing pinned any of it.
//
// That matters more here than for most sections, because this doc replaced a
// paragraph that had gone FALSE — it said `benchmarks/repo-state-v1/` was not runnable,
// which stopped being true the moment the loader landed. A doc that has already drifted
// once, corrected in prose only, will drift again just as silently.
//
// The guard is BIDIRECTIONAL, like its sibling benchmark_publishable_doc_test.go:
// asserting only that the doc names a key catches a doc edit but not a code edit, and
// the code direction is the one that misleads a reader — the doc keeps promising a
// field the run-result no longer emits. Each claim is therefore asserted twice, once
// against the doc and once against the struct tag or code arm that makes it true.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift tests
// (no Go package lives under docs/, and the published reconcile/ module must not assume
// this repo's file layout). See justification_record_boundary_test.go for the precedent.
func TestBenchmarkDoc_RepoStateSectionMatchesTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	score := readRepoFile(t, "../../internal/benchmark/score_repostate.go")
	runResult := readRepoFile(t, "../../internal/benchmark/benchmark.go")
	cli := readRepoFile(t, "../../cli/benchmark_repostate.go")

	// The array's own key. The doc tells a reader where to look in the run-result;
	// a renamed struct tag would leave them looking for a key that is not there.
	assert.Contains(t, doc, "reviewer_positional_recall",
		"the doc must name the run-result array that carries the metric")
	assert.Contains(t, runResult, `json:"reviewer_positional_recall,omitempty"`,
		"RunResult must still emit the array the doc names")

	// The three rates, each documented as a row of a table. A reader copies these
	// names into a jq expression, so a drifted name is a silently empty result.
	// Matched as a BACKTICKED table cell, not a bare substring: "recall" is a
	// substring of the other two keys and appears in at least eight unrelated
	// sentences in this doc, so a bare-word assertion verified nothing about the
	// recall row — a renamed or deleted cell would have passed untouched.
	for _, key := range []string{"recall", "outside_diff_recall", "within_diff_recall"} {
		assert.Contains(t, doc, "`"+key+"`", "the doc must carry %q as a table cell", key)
		assert.Contains(t, score, `json:"`+key+`,omitempty"`,
			"ReviewerPositionalRecall must still emit %q, with omitempty — the doc promises an "+
				"ABSENT key rather than a zero when nothing was expected", key)
	}

	// The unmeasured-vs-zero rule. This is the claim a consumer acts on when deciding
	// whether a 0 means "missed everything" or "nothing was planted", so the doc saying
	// it and the code doing it must not come apart.
	//
	// Anchored on SEMANTIC substrings, not gofmt-level formatting: the previous pin
	// matched "if expected <= 0 {\n\t\treturn nil\n\t}" — indentation and newlines
	// included — so any reflow of rate() broke a test that has nothing to do with
	// docs. "expected <= 0" and "return nil" are individually gofmt-stable and
	// together still pin the nil-when-unmeasured arm. The behavior itself is not
	// executable from this module (rate() is unexported in the parent module's
	// internal tree), so the source-semantics anchor is the strongest pin available
	// here; the executable half belongs to internal/benchmark's own test suite.
	assert.Contains(t, doc, "an unmeasured rate must not read as a measured zero",
		"the doc must state the unmeasured-vs-zero rule")
	assert.Contains(t, score, "expected <= 0",
		"rate() must still gate on the expected denominator")
	assert.Contains(t, score, "return nil",
		"rate() must still return nil rather than 0 when nothing was expected")

	// --checkpoint is REFUSED, not ignored. A doc that said "ignored" while the code
	// refused would be merely wrong; a doc that says "refused" while the code silently
	// ignores would cost an operator a whole panel run.
	assert.Contains(t, doc, "`--checkpoint` is rejected, not ignored",
		"the doc must state that the flag is refused")
	// Anchored on the message's stable prefix, not the %s verb: the refusal's
	// suite-format interpolation is an implementation detail, and pinning the raw
	// format string broke on any reword that kept the operator-visible sentence.
	assert.Contains(t, cli, "--checkpoint is not supported for a",
		"checkRepoStateFlags must still return an error for the flag")

	// The grounding gate stays ON. This is the design decision the whole tier's
	// headline number depends on, and the one a future reader is most likely to
	// mistake for an oversight and "fix".
	assert.Contains(t, doc, "grounding gate stays ON",
		"the doc must record that the Epic 14.1 gate is deliberately left enabled")
	assert.Contains(t, cli, "THE EPIC 14.1 GROUNDING GATE STAYS ON",
		"the runner must still carry the rationale the doc points at")

	// The range path, not the diff path. A future refactor routing repo-state cases
	// through PrepareReviewFromDiff would silently stop measuring the two sibling
	// epics this tier exists to measure, and the doc would still claim otherwise.
	assert.Contains(t, doc, "It reviews a real git range, not an ingested diff",
		"the doc must state which review path the tier uses")
	assert.Contains(t, cli, "fanout.PrepareReview(ctx, cfg, req)",
		"the runner must still drive the RANGE path")
	// Matched as a CALL — "fanout.PrepareReviewFromDiff(" — not as a bare mention.
	// The runner's own doc comment names the function it deliberately does not use,
	// explaining why, and a bare NotContains would forbid that explanation.
	assert.NotContains(t, cli, "fanout.PrepareReviewFromDiff(",
		"routing repo-state cases through diff ingestion would drop the RangeBuilder, "+
			"and with it the claim ledger and pre-fetching this tier measures")
}

// The stale claim this section replaced must not come back. It is asserted by
// ABSENCE because that is how it would return: a future editor restoring an old
// paragraph, or copying the caveat from a pre-35.16.10 revision.
func TestBenchmarkDoc_NoLongerClaimsRepoStateIsUnrunnable(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")

	for _, stale := range []string{
		"no loader reads them yet",
		"neither validates nor runs it",
	} {
		assert.NotContains(t, doc, stale,
			"docs/benchmark.md still claims repo-state-v1 is not runnable, which stopped "+
				"being true when the loader landed in epic 35.16.10")
	}
}

// readRepoFileOrSkip is deliberately absent: a missing file is a real failure here,
// not a reason to skip. Every path above is tracked in this repository.
var _ = os.ReadFile

// The shipped suite must actually contain the documents this epic's AC6 requires.
// A SPOT-CHECK.md that was never written is the failure mode AC6 exists to prevent,
// and it is invisible to every other test in the tree.
//
// AC6 is "every case hand-verified in SPOT-CHECK.md" — existence and non-emptiness
// do not deliver it: a file holding a single byte passed the old check, a fifth
// case added to the manifest with no spot-check section passed, and a suite_version
// bump stranding the doc's quoted version passed. Each case id must therefore
// appear as a RESULTS heading, and the doc's title must quote the manifest's
// CURRENT suite_version (the historical run block legitimately still quotes the
// version the run was recorded at, so the anchor is the title, not the whole file).
func TestRepoStateSuite_ShipsItsVerificationDocuments(t *testing.T) {
	for _, path := range []string{
		"../../benchmarks/repo-state-v1/SPOT-CHECK.md",
		"../../benchmarks/repo-state-v1/NOTICE.md",
	} {
		data, err := os.ReadFile(path)
		require.NoError(t, err, "the suite must ship %s", path)
		assert.NotEmpty(t, data)
	}

	manifestData, err := os.ReadFile("../../benchmarks/repo-state-v1/suite.json")
	require.NoError(t, err)
	var manifest struct {
		Suite        string `json:"suite"`
		SuiteVersion string `json:"suite_version"`
		Cases        []struct {
			ID  string `json:"id"`
			Dir string `json:"dir"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(manifestData, &manifest))
	require.NotEmpty(t, manifest.Cases, "the manifest must declare its cases")

	spot, err := os.ReadFile("../../benchmarks/repo-state-v1/SPOT-CHECK.md")
	require.NoError(t, err)
	doc := string(spot)

	// Every manifest case must have its own hand-verification heading (AC6's core).
	for _, c := range manifest.Cases {
		assert.Contains(t, doc, "### `"+c.ID+"`",
			"SPOT-CHECK.md must carry a results heading naming case %q — a case with no "+
				"section was never hand-verified, which is exactly what AC6 forbids", c.ID)
	}

	// The title's quoted version must equal the manifest's, so a suite_version bump
	// cannot strand the spot-check at a stale number unnoticed.
	titleRe := regexp.MustCompile("# Spot-check — `repo-state-v1` ([0-9]+\\.[0-9]+\\.[0-9]+)")
	m := titleRe.FindStringSubmatch(doc)
	require.NotNil(t, m, "SPOT-CHECK.md title must quote the suite version it was checked at")
	assert.Equal(t, manifest.SuiteVersion, m[1],
		"SPOT-CHECK.md was checked at %s but the manifest is now %s — re-run the hand-check "+
			"or bump the doc before shipping the suite at a version its verification does not cover",
		m[1], manifest.SuiteVersion)
}
