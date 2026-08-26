package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRunResultWithReviewers writes a minimal export-valid run-result carrying the
// given reviewer rows verbatim, so a test can supply an identity that is non-empty
// RAW but empty once scrubbed.
func writeRunResultWithReviewers(t *testing.T, revs []scorecard.PublicRecord) string {
	t.Helper()
	rr := benchmark.RunResult{
		Suite:        "suite-valid",
		SuiteVersion: "1",
		GeneratedAt:  "2026-08-15T00:00:00Z",
		Reviewers:    revs,
	}
	data, err := json.MarshalIndent(rr, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// The export gate's stated invariant is that "an unidentifiable reviewer row on a
// public leaderboard is worse than a rejected file". It checked the RAW identity while
// BuildSubmission serialized the SCRUBBED one, so an identity that survives the check
// but scrubs to "" was published unidentifiable.
//
// The gap is not hypothetical: scrubField iterates to a fixed point, and one pass can
// EXPOSE a match for an earlier rule — removing the email-shaped prefix of
// "bedrock@us-east-1/claude" leaves "/claude", which the next pass reads as an
// absolute path. Every input below is non-empty raw and empty scrubbed.
func TestBenchmarkExport_RejectsIdentityThatScrubsToNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model string
	}{
		{"email-shaped prefix over a path-shaped suffix", "bedrock@us-east-1/claude"},
		{"proxy host over a provider-prefixed id", "proxy@lan/openai/gpt-4o"},
		{"credential-shaped prefix over a path-shaped suffix", "sk-abc/claude"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Guard the premise: raw is non-empty, scrubbed is empty. If scrubField
			// ever stops emptying these, this test is asserting nothing.
			require.NotEmpty(t, tc.model)
			require.Empty(t, scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: tc.model, Persona: "p-a"}).Model,
				"premise broken: %q no longer scrubs to empty, so this case cannot reach the defect", tc.model)

			path := writeRunResultWithReviewers(t, []scorecard.PublicRecord{{Model: tc.model, Persona: "p-a"}})
			stdout, _, err := execExportErr(t, path)

			require.Error(t, err, "an identity that publishes as \"\" must be rejected, not published")
			// The operator has to be able to find the offending row in their file, so the
			// message names the PRE-scrub string — `"" is empty` is undebuggable.
			assert.Contains(t, err.Error(), tc.model)
			assert.Empty(t, stdout, "no submission may be emitted for a rejected file")
		})
	}
}

// The persona half of the same identity gets the same rule — it is scrubbed by the
// same function and joined on by the same coverage key.
func TestBenchmarkExport_RejectsPersonaThatScrubsToNothing(t *testing.T) {
	const persona = "bedrock@us-east-1/sonny"
	require.Empty(t, scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: "m-a", Persona: persona}).Persona,
		"premise broken: %q no longer scrubs to empty", persona)

	path := writeRunResultWithReviewers(t, []scorecard.PublicRecord{{Model: "m-a", Persona: persona}})
	stdout, _, err := execExportErr(t, path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), persona)
	assert.Empty(t, stdout)
}

// The gate must not start rejecting identities the producer legitimately emits: a
// provider-prefixed model id survives the scrub unchanged and has to keep publishing.
func TestBenchmarkExport_AcceptsIdentityThatSurvivesTheScrub(t *testing.T) {
	path := writeRunResultWithReviewers(t, []scorecard.PublicRecord{
		{Model: "anthropic/claude-3", Persona: "sonny"},
		{Model: "qwen3.8-max", Persona: "brad"},
	})
	stdout, _, err := execExportErr(t, path)

	require.NoError(t, err)
	assert.Contains(t, stdout, "anthropic/claude-3")
	assert.Contains(t, stdout, "qwen3.8-max")
}

// The reviewer-identity gate is the third arm of the same publication predicate the
// suite identity (validateSuiteIdentityForPublication) and the case ids
// (validateScrubbedCaseIDs) already carry, and it was the one left checking only the
// scrubs-to-empty half.
//
// Every input below is non-empty RAW and non-empty SCRUBBED — ScrubPublicString leaves
// control and format runes untouched — so the empty-only gate cannot see them and no
// sibling gate catches the row on the way out. A leaderboard row rendering
// "gpt-4<U+202E>evil" misattributes results to a model that was never measured.
func TestBenchmarkExport_RejectsReviewerIdentityWithNonPrintingRune(t *testing.T) {
	for _, tc := range []struct {
		name    string
		model   string
		persona string
		offend  string
		rune    string
	}{
		{"bidi override in the model", "gpt-4\u202Eevil", "p-a", "gpt-4\u202Eevil", "U+202E"},
		{"zero-width space in the persona", "m-a", "bra\u200Bd", "bra\u200Bd", "U+200B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Guard the premise: the empty-only gate must NOT be able to catch these,
			// or the test would pass against the unfixed code for the wrong reason.
			pub := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: tc.model, Persona: tc.persona})
			require.NotEmpty(t, pub.Model, "premise broken: %q now scrubs to empty, so the pre-existing gate catches it", tc.model)
			require.NotEmpty(t, pub.Persona, "premise broken: %q now scrubs to empty, so the pre-existing gate catches it", tc.persona)

			path := writeRunResultWithReviewers(t, []scorecard.PublicRecord{{Model: tc.model, Persona: tc.persona}})
			stdout, _, err := execExportErr(t, path)

			require.Error(t, err, "a reviewer identity carrying an invisible rune must not reach the published envelope")
			// Same diagnostic shape as the suite and case-id gates: the PRE-scrub value
			// under %q so the operator can find the row, and the rune under U+%04X so
			// they can see what is in it. strconv.Quote is what %q renders, escape and
			// all - asserting the raw string would assert the invisible rune reached the
			// terminal unescaped, which is the opposite of what this gate is for.
			assert.Contains(t, err.Error(), strconv.Quote(tc.offend))
			assert.Contains(t, err.Error(), tc.rune)
			assert.Empty(t, stdout, "no submission may be emitted for a rejected file")
		})
	}
}
