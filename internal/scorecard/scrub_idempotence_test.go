package scorecard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// scrubField is applied more than once to the same identity by construction, not by
// accident: cli/benchmark_run.go scrubs at the fold, then benchmark.Score,
// benchmark.PerReviewerVocabulary and benchmark.BuildSubmission each re-scrub as
// defense-in-depth. Those layers are only safe if scrubbing twice equals scrubbing
// once — otherwise the SAME reviewer ends up with different values in different
// arrays of one run-result, and reviewer_coverage[i] stops naming the reviewer that
// reviewers[i] and reviewer_vocabulary[i] describe.
//
// It was NOT idempotent: scrubEmail runs after scrubAbsPath, so removing an
// email-shaped prefix could expose a leading '/' that only a LATER pass would strip.
// "bedrock@us-east-1/claude" scrubbed once to "/claude" and twice to "".
func TestScrubField_IsIdempotent(t *testing.T) {
	inputs := []string{
		// The measured divergence: an email-shaped region prefix before a '/'.
		"bedrock@us-east-1/claude",
		"openai@proxy/gpt-4",
		"gpt-4 sk-abcdef/x",
		// Ordinary provider-prefixed ids, which must survive unchanged.
		"openai/gpt-4",
		"anthropic/claude-3",
		"bedrock/anthropic.claude-v2",
		"vertex_ai/gemini",
		"azure/gpt-4o",
		"openrouter/qwen/qwen3-max",
		"qwen3.8-max",
		"kimi-k3",
		"glm-5.2",
		"brad",
		"",
		// Shapes the backstop exists to strip — still must reach a FIXED POINT.
		"/Users/sam/model",
		"user@example.com",
		"model ~/.config/x",
		`C:\Users\sam\m`,
		"host/etc/passwd",
		"sk-deadbeefcafe",
	}

	for _, in := range inputs {
		once := scrubField(in)
		twice := scrubField(once)
		assert.Equal(t, once, twice,
			"scrubField(%q) = %q but scrubbing that again gives %q — the re-scrub layers in "+
				"Score / PerReviewerVocabulary / BuildSubmission then disagree with the fold, "+
				"and one reviewer gets two identities in one run-result", in, once, twice)
	}
}

// ScrubPublicString is the field-level scrub exported for non-identity values that
// share the envelope and its privacy contract — benchmark suite case ids, which the
// CLI gate and BuildSubmission scrub through the same rules. It must be exactly the
// Model-field scrub, no more and no less: a dedicated case-id scrub would diverge,
// and laundering ids through a synthetic PublicRecord couples them to Model-specific
// semantics.
func TestScrubPublicString_MatchesTheIdentityFieldScrub(t *testing.T) {
	inputs := []string{
		"case-01-nil-deref",
		"sk-io-pr-42",
		"case-01 /Users/sam/secret.txt",
		"bedrock@us-east-1/claude",
		"",
	}
	for _, in := range inputs {
		assert.Equal(t, ScrubPublicRecord(PublicRecord{Model: in}).Model, ScrubPublicString(in),
			"the exported string scrub must be exactly the Model-field scrub for %q", in)
	}
}

// The identity-parity consequence, stated at the level the run-result cares about:
// ScrubPublicRecord must land on a fixed point too, since that is the call every
// layer actually makes.
func TestScrubPublicRecord_IsIdempotent(t *testing.T) {
	for _, in := range []string{"bedrock@us-east-1/claude", "openai@proxy/gpt-4", "openai/gpt-4", "brad"} {
		once := ScrubPublicRecord(PublicRecord{Model: in, Persona: in})
		twice := ScrubPublicRecord(once)
		assert.Equal(t, once.Model, twice.Model, "Model %q is not a fixed point", in)
		assert.Equal(t, once.Persona, twice.Persona, "Persona %q is not a fixed point", in)
	}
}
