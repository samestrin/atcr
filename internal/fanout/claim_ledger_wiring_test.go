package fanout

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The claim ledger (Epic 35.16.7) is produced in internal/payload and reaches
// reviewers only if it survives buildPayloads' global shed, buildSlots' per-agent
// shed, and prompt rendering — none of which live in that package. Proving AC1
// ("the ledger reaches the payload") and AC3 ("identical for every agent") with
// unit tests inside internal/payload alone would assert the value at the point it
// is produced and never at the point it is consumed: the "passes a unit test,
// fails in production" shape /refine-epic flagged for this epic.
//
// This file is test-only: it adds no production code and changes no behavior.
// AC6's freeze on internal/fanout is being read as "no production-code change":
// the package tree does gain this file — strictly a change to the component —
// so the freeze holds by that reading, not by an untouched tree. The file lives
// here anyway because the assertions must observe the ledger where it is
// CONSUMED (buildPayloads' shed, buildSlots' per-agent shed, prompt rendering),
// and those seams are package-private to internal/fanout.

// extractLedger returns the CLAIMS TO VERIFY block from a rendered prompt.
func extractLedger(t *testing.T, prompt string) string {
	t.Helper()
	const head = "## CLAIMS TO VERIFY\n"
	const end = "----- END CLAIMS -----\n"
	i := strings.Index(prompt, head)
	require.GreaterOrEqual(t, i, 0, "prompt carries no claim ledger")
	j := strings.Index(prompt[i:], end)
	require.GreaterOrEqual(t, j, 0, "claim ledger is not closed")
	return prompt[i : i+j+len(end)]
}

// AC1 + AC3 end to end: the ledger built in internal/payload survives every
// shed between buildPayloads and a rendered prompt, and every agent in the
// fan-out receives byte-identical claim text.
func TestClaimLedger_ReachesEveryAgentsRenderedPrompt(t *testing.T) {
	dir, base, head := fanoutRepo(t)

	cfg := sizingRosterConfig() // two agents, deliberately different context windows
	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	rng := ReviewRange{Base: base, Head: head}
	slots, _, err := buildSlots(cfg, payloads, rng, "", "", false)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(slots), 2, "precondition: the roster must fan out to more than one agent")

	first := extractLedger(t, slots[0].Primary.Prompt)
	assert.Contains(t, first, "Begin() no longer returns a wiped offset")
	assert.Contains(t, first, "Drain() keeps the previous offset")
	assert.Contains(t, first, "UNSUPPORTED")

	for _, s := range slots {
		assert.Equal(t, first, extractLedger(t, s.Primary.Prompt),
			"agent %q must receive a byte-identical claim ledger", s.Primary.Name)
	}
}

// The ledger's whole purpose is to survive a shed that drops diff content. The
// budget is derived from the fixture's own entry sizes so the test pins the
// SOME-but-not-all shed rather than accidentally landing on either extreme.
func TestClaimLedger_SurvivesAByteBudgetThatShedsDiffContent(t *testing.T) {
	dir, base, head := fanoutRepo(t)

	cfg := sizingRosterConfig()
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}

	// Learn the real entry sizes first, then budget for the smallest reviewable
	// file alone. A hardcoded number would silently become an all-dropped or a
	// nothing-dropped run the day the fixture's rendering changes by a byte.
	unlimited, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)
	var smallest int64
	for _, mp := range unlimited {
		for _, e := range mp.Entries {
			if e.Path == payload.ClaimLedgerPath {
				continue
			}
			if smallest == 0 || e.Size < smallest {
				smallest = e.Size
			}
		}
	}
	require.NotZero(t, smallest, "precondition: the fixture must produce sized entries")

	cfg.Settings.PayloadByteBudget = smallest + 1
	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	var shed bool
	for _, mp := range payloads {
		if mp.Truncation.Truncated {
			shed = true
			assert.NotContains(t, mp.Truncation.FilesDropped, payload.ClaimLedgerPath,
				"the ledger is exempt: it must never appear in the shed record")
		}
	}
	require.True(t, shed, "precondition: the budget must actually shed a file")

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
	require.NoError(t, err)
	require.NotEmpty(t, slots)

	for _, s := range slots {
		assert.Contains(t, extractLedger(t, s.Primary.Prompt), "Begin() no longer returns a wiped offset")
	}
}

// paddedClaimingRepo is the two-commit claiming fixture with both changed files
// padded far past the small window's effective byte budget, so a 32768-window
// agent cannot inherit the primary's payload whole and must re-fit (or take the
// zero-budget arm). The claiming message rides the padded commit, so the
// ledger's claims still describe the same two behaviors.
func paddedClaimingRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	fanoutGit(t, dir, "init", "-q", "-b", "main")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "cursor.go"), []byte("package p\n\nfunc Begin() int { return 0 }\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "seed the cursor file")
	base = fanoutGit(t, dir, "rev-parse", "HEAD")

	pad := strings.Repeat("// padding to size the payload past a small window's byte budget\n", 800)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cursor.go"), []byte("package p\n\nfunc Begin() int { return 1 }\n"+pad), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "drain.go"), []byte("package p\n\nfunc Drain() int { return Begin() }\n"+pad), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "preserve the cursor across a cold drain\n\n- Begin() no longer returns a wiped offset\n- Drain() keeps the previous offset\n")
	head = fanoutGit(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

// The fallback re-fit (on_overflow=truncate) re-packs the slot's entries against
// the FALLBACK's own budget — and re-sizes every entry to len(Body) on the way
// in, which is the one path where the ledger's exemption has to survive being
// counted like any other file. A mistake there drops the ledger from SOME
// agents' prompts: an asymmetry that looks exactly like a branch that claimed
// nothing, the failure AC3 forbids. No test drove a re-fit with a ledger
// present; this one pins the invariant — the ledger a re-fit fallback re-renders
// is byte-identical to the primary's.
//
// RED note: the invariant HOLDS in the current code, so this test is green on
// arrival — the row's issue is the missing coverage, not broken behavior. The
// row's verify step (falsify by breaking the shed exemption) would require a
// transient write to internal/payload/budget.go, owned by another live
// resolve-td session's group scope, so that mutation check is deferred to the
// ungrouped follow-up run.
func TestClaimLedger_SurvivesAFallbackRefit(t *testing.T) {
	dir, base, head := paddedClaimingRepo(t)

	cfg := sizingRosterConfig()
	kai := cfg.Registry.Agents["kai"]
	kai.Fallback = "greta" // greta: unlisted-small-model → 32768 window, far under kai's 128000
	cfg.Registry.Agents["kai"] = kai
	cfg.Project.Agents = []string{"kai"}
	cfg.Settings.OnOverflow = OverflowTruncate

	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	var slots []Slot
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
	})
	require.NoError(t, err)
	require.NotEmpty(t, slots, "precondition: the roster must produce a slot")
	s := slots[0]
	require.NotEmpty(t, s.Fallbacks, "precondition: kai must resolve its greta fallback")

	primary := extractLedger(t, s.Primary.Prompt)
	for _, fb := range s.Fallbacks {
		require.NotEqual(t, s.Primary.Prompt, fb.Prompt,
			"precondition: the fallback must have re-fit (re-rendered) rather than inheriting the primary's prompt")
		require.True(t, fb.Truncation.Truncated,
			"precondition: the re-fit must actually shed a file, or this test proves nothing about the re-fit path")
		assert.NotContains(t, fb.Truncation.FilesDropped, payload.ClaimLedgerPath,
			"the ledger fits the fallback's budget: the exemption must keep it out of the re-fit's shed record")
		assert.Equal(t, primary, extractLedger(t, fb.Prompt),
			"the re-fit fallback must carry a byte-identical claim ledger")
	}
}

// Accepted consequence #3 (claims.go, recorded where the ledger entry is born):
// an agent whose declared window drives its effective budget to 0 takes the
// keepSmallestEntry arm, which picks by len(Body) with no ledger awareness and
// may ship the ledger as the SOLE entry — a reviewer holding claims and no
// code. That outcome is accepted, mitigated by the section's own NOT-IN-PAYLOAD
// instruction; what was never tested is the mitigation itself. This test
// realizes the risk shape — the padded fixture makes the ledger the smallest
// non-empty entry, so the zero-budget arm keeps it alone — and pins the
// contract: the prompt that reviewer actually holds must tell it to answer
// NOT-IN-PAYLOAD rather than manufacture a sheet of false UNSUPPORTED findings.
//
// RED note: the behavior HOLDS in the current code (the test is green on
// arrival; the row's issue is the untested mitigation). The row's verify step —
// mutating the NOT-IN-PAYLOAD sentence out of claimLedgerSection and confirming
// this test fails — requires a transient write to internal/payload/claims.go,
// owned by another live resolve-td session's group scope, so the mutation check
// is deferred to the ungrouped follow-up run.
func TestClaimLedger_ZeroBudgetRefitShipsTheNotInPayloadContract(t *testing.T) {
	dir, base, head := paddedClaimingRepo(t)

	cfg := sizingRosterConfig()
	greta := cfg.Registry.Agents["greta"]
	mt := 29000 // 32768 floor window − 29000 − 4096 overhead ≤ 0 → effective budget 0
	greta.MaxTokens = &mt
	cfg.Registry.Agents["greta"] = greta
	kai := cfg.Registry.Agents["kai"]
	kai.Fallback = "greta"
	cfg.Registry.Agents["kai"] = kai
	cfg.Project.Agents = []string{"kai"}
	cfg.Settings.OnOverflow = OverflowTruncate

	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	var slots []Slot
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
	})
	require.NoError(t, err)
	require.NotEmpty(t, slots, "precondition: the roster must produce a slot")
	s := slots[0]
	require.Len(t, s.Fallbacks, 1, "precondition: kai must resolve exactly one fallback (greta)")
	fb := s.Fallbacks[0]

	// The zero-budget arm kept the ledger as the SOLE entry: both padded code
	// files are gone and no code body reaches the reviewer.
	assert.ElementsMatch(t, []string{"cursor.go", "drain.go"}, fb.Truncation.FilesDropped,
		"the zero-budget re-fit must shed both padded code files, keeping the ledger alone")
	assert.NotContains(t, fb.Prompt, "func Begin",
		"the reviewer holds claims and no code — the exact consequence #3 accepts")

	// The mitigation: the ledger the reviewer holds must carry the
	// NOT-IN-PAYLOAD contract, byte-identical to the primary's.
	primary := extractLedger(t, s.Primary.Prompt)
	fbLedger := extractLedger(t, fb.Prompt)
	assert.Equal(t, primary, fbLedger, "the sole kept entry must be the ledger, byte-identical to the primary's")
	assert.Contains(t, fbLedger,
		"If the payload contains no code at all, answer NOT-IN-PAYLOAD for every claim and report nothing.",
		"the reviewer holding claims and no code must be told to answer NOT-IN-PAYLOAD and report nothing")
}

// The exempt ledger must NOT quietly convert a fully-shed payload into a
// dispatchable one. A reviewer holding claims and no code returns a false-clean
// "no findings" review, so the run has to fail loudly instead — which it only
// does because Truncation.AllDropped counts reviewable files rather than kept
// entries.
func TestClaimLedger_DoesNotMaskAFullyShedPayload(t *testing.T) {
	dir, base, head := fanoutRepo(t)

	cfg := sizingRosterConfig()
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 1 // funds nothing at all

	_, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.ErrorIs(t, err, ErrPayloadFullyDropped,
		"a payload carrying only the ledger must still be reported as fully dropped")
}

// A branch whose commits assert nothing must render no section at all, so the
// engine never puts words in an author's mouth.
func TestClaimLedger_AbsentWhenTheBranchAssertsNothing(t *testing.T) {
	dir := t.TempDir()
	fanoutGit(t, dir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "seed")
	base := fanoutGit(t, dir, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n\nvar X = 1\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "wip")
	head := fanoutGit(t, dir, "rev-parse", "HEAD")

	cfg := sizingRosterConfig()
	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	for _, s := range slots {
		assert.NotContains(t, s.Primary.Prompt, "CLAIMS TO VERIFY")
		assert.NotContains(t, s.Primary.Prompt, payload.ClaimLedgerPath)
	}
}
