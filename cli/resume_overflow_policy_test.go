package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// zeroInputBudgetResumeFixture stands up a resumable review whose CONFIG has since
// grown a max_tokens declaration large enough to leave the agent no input budget at
// all (32000 against the 32768-token default window, minus the 4096-token prompt
// reserve), under review_strategy=chunked with a hard on_overflow policy. cfg is
// re-loaded live at resume time, so buildSlots re-derives that zero budget for the
// still-pending agent and the chunked zero-budget arm dispatches the policy.
func zeroInputBudgetResumeFixture(t *testing.T, policy string) {
	t.Helper()
	initGitRepoWithChange(t)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(`providers:
  testprov:
    api_key_env: ATCR_TEST_REVIEW_KEY
    base_url: http://127.0.0.1:1/v1
agents:
  bruce:
    provider: testprov
    model: test-model
    max_tokens: 32000
`), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents: [bruce]\nreview_strategy: chunked\non_overflow: "+policy+"\n"), 0o644))

	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	// bruce is in the roster but NOT completed, so PrepareResume rebuilds a pending
	// slot for it — the path that reaches the chunked zero-budget arm.
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce"}, nil)
}

// A review whose per-agent artifacts are intact must not be made unresumable by an
// unreadable error. The chunked zero-budget arm hard-fails pre-dispatch by design
// (that is the on_overflow contract), but the error surfacing at the CLI was the raw
// dispatch sentinel — "model-swap fallback requires fallback-provenance recording
// (F5) and is not dispatched here" for fallback, or the bare fail sentinel — neither
// of which names the declaration the operator has to change. The stale-review message
// meanwhile offers `atcr review --resume <id>` as the recovery path, so the operator
// is sent back into the invocation that just failed with nothing to act on.
func TestResume_ZeroInputBudgetNamesTheDeclarationToChange(t *testing.T) {
	for _, policy := range []string{"fail", "fallback"} {
		t.Run(policy, func(t *testing.T) {
			isolate(t)
			t.Setenv(testReviewKeyEnv, "secret")
			zeroInputBudgetResumeFixture(t, policy)

			code, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")

			require.Equal(t, 2, code, "a config the run cannot satisfy is a config error")
			require.Contains(t, out, "max_tokens",
				"the operator must be told WHICH declaration makes the budget zero")
			require.Contains(t, out, "context_window_tokens",
				"raising the declared window is the other half of the remedy")
			require.Contains(t, out, "bruce",
				"the agent whose budget collapsed must be named")
			// The dispatch sentinel may remain in the chain (errors.Is still has to
			// match it), but it must not be the FIRST thing the operator reads —
			// leading with it is what made this exit unreadable.
			//
			// Both indices are asserted PRESENT first. Comparing them straight would
			// pass vacuously if the diagnosis wording changed (a missing marker yields
			// -1, which is less than any real index) — the assertion would then hold
			// precisely when the diagnosis had disappeared.
			diag, sentinel := indexOfDiagnosis(out), indexOfSentinel(out)
			require.NotEqual(t, -1, diag, "the actionable diagnosis must be present at all")
			require.NotEqual(t, -1, sentinel, "precondition: the wrapped sentinel is still in the chain")
			require.Less(t, diag, sentinel,
				"the actionable diagnosis must precede the internal policy sentinel")
		})
	}
}

// --max-tokens threads through --resume LIVE, by the same mechanism as --timeout and
// --max-parallel: runResume builds the config with the same cliOverrides(cmd) helper
// runReview uses, so the CLI tier reaches maxTokensFor on the resume path too.
//
// This is pinned because a TD row argued the opposite — that the cap is discarded on a
// resume and must therefore be manifest-recorded or rejected fail-closed. The premise was
// wrong, but nothing enforced it: the same fixture that hard-fails on the agent's 32000
// declaration must stop failing when the operator retypes a workable --max-tokens, and if
// the override were ever dropped from cliOverrides that distinction would vanish silently
// and the row would become correct.
func TestResume_MaxTokensOverrideAppliesToPendingAgents(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	// The roster declares max_tokens 32000, which leaves no input budget against the
	// 32768-token default window and hard-fails under on_overflow=fail.
	zeroInputBudgetResumeFixture(t, "fail")

	// Retyping a workable cap on the resume must override that declaration, so the
	// zero-budget refusal never fires. The run still fails at dispatch (the fixture
	// provider is unreachable by design) — the assertion is on WHICH failure.
	_, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^", "--max-tokens", "2048")

	require.NotContains(t, out, "no input budget",
		"a retyped --max-tokens must resize the pending agents on a resume, exactly as --timeout does")
	require.NotContains(t, out, "on_overflow=fail",
		"with a funded budget the overflow policy must not be consulted at all")
}

// indexOfDiagnosis / indexOfSentinel locate the operator-actionable text and the
// internal on_overflow sentinel in combined output. Both return -1 when absent, and the
// caller asserts presence before comparing — a "not found" sentinel that sorts before
// every real index would make the ordering assertion pass exactly when the text it
// checks for had gone missing.
//
// Keyed on the agent-naming prefix every refusal site shares (refuseOverflow), not on a
// particular phrasing of the budget clause, so rewording the diagnosis does not silently
// disarm the test.
func indexOfDiagnosis(out string) int { return strings.Index(out, `agent "`) }

func indexOfSentinel(out string) int { return strings.Index(out, "on_overflow=") }
