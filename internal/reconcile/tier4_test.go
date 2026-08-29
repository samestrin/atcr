package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTier4 is a tier4Resolver whose verdict is scripted per anchor, so the
// wiring in validateFindingPaths can be tested without parsing anything.
type fakeTier4 struct {
	byAnchor map[string]string // anchor -> resolved file
	inconc   map[string]bool   // anchor -> report inconclusive
	calls    int
}

func (f *fakeTier4) resolve(anchors []string) (string, tier4Outcome) {
	f.calls++
	if len(anchors) == 0 {
		return "", tier4Inconclusive
	}
	for _, a := range anchors {
		if f.inconc[a] {
			return "", tier4Inconclusive
		}
		if file, ok := f.byAnchor[a]; ok {
			return file, tier4Resolved
		}
	}
	return "", tier4NoMatch
}

// withFakeTier4 swaps the Tier 4 index constructor for the duration of a test
// and reports how many times one was CONSTRUCTED — the AC5 signal, since
// construction is what precedes any wazero work.
func withFakeTier4(t *testing.T, f *fakeTier4) *int {
	t.Helper()
	built := 0
	prev := newTier4Index
	newTier4Index = func(root string, paths []string) tier4Resolver {
		built++
		return f
	}
	t.Cleanup(func() { newTier4Index = prev })
	return &built
}

// tier4Repo builds a throwaway git repo (the candidate index reads
// `git ls-files`) holding the given tracked files.
func tier4Repo(t *testing.T, relpaths ...string) string {
	t.Helper()
	return gitRepoWithFiles(t, relpaths...)
}

// TestTier4_PromotesDissimilarFilename is AC1: the cited file does not exist and
// has no filename-level candidate (5.4 Tiers 1-3 all miss on a dissimilar name),
// but the construct the finding describes resolves to exactly one tracked file.
func TestTier4_PromotesDissimilarFilename(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{byAnchor: map[string]string{"RefreshToken": "internal/auth/session.go"}}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/tokens/renewal.go",
		Line:    31,
		Problem: "`RefreshToken` never checks the expiry before reissuing",
		Fix:     "compare against the issued-at claim first",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.False(t, findings[0].PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, findings[0].PathWarning)
	assert.Equal(t, "internal/auth/session.go", findings[0].PathSuggestion)
	assert.Equal(t, "internal/tokens/renewal.go", findings[0].File, "AC7 of 5.4 still holds: File is never rewritten")
	assert.Empty(t, unresolved, "a resolved finding is never sidecar-routed")
	assert.Equal(t, 1, *built)
}

// TestTier4_NoMatchIsSidecarEligible is AC3's input condition: all four tiers
// exhaust with zero symbol correspondence, so the finding is reported back for
// sidecar routing.
func TestTier4_NoMatchIsSidecarEligible(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{byAnchor: map[string]string{"RefreshToken": "internal/auth/session.go"}}
	withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Line:    9,
		Problem: "`quantumFlux` leaks a handle on every retry",
		Fix:     "close it in `quantumFlux`",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.Equal(t, []int{0}, unresolved)
	assert.Empty(t, findings[0].PathSuggestion, "a no-match never invents a suggestion")
}

// TestTier4_InconclusiveStaysInPrimary pins the clarified rule that AC7 beats
// the plan's "fall through to step 4" prose: an anchor matching more than one
// file is real code, so the finding keeps its place in the primary stream with
// no suggestion.
func TestTier4_InconclusiveStaysInPrimary(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go", "internal/net/pool.go")
	fake := &fakeTier4{inconc: map[string]bool{"Close": true}}
	withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "`Close` is called twice on the same handle",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.Empty(t, unresolved)
	assert.Empty(t, findings[0].PathSuggestion)
}

// TestTier4_NotConsultedWhenTiers123Suggested pins the gating: Tier 4 runs only
// when the filename-level tiers produced nothing, and never overwrites their
// answer.
func TestTier4_NotConsultedWhenTiers123Suggested(t *testing.T) {
	root := tier4Repo(t, "internal/auth/validate.go")
	fake := &fakeTier4{byAnchor: map[string]string{"ValidatePath": "internal/net/pool.go"}}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/auth/validator.go", // Tier 2 typo hit
		Problem: "`ValidatePath` swallows the error",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.Equal(t, "internal/auth/validate.go", findings[0].PathSuggestion, "the Tier 2 answer stands")
	assert.Empty(t, unresolved)
	assert.Zero(t, fake.calls, "Tier 4 is not consulted once Tiers 1-3 answered")
	assert.Zero(t, *built)
}

// TestTier4_NotConsultedForValidPaths is AC5: a run whose findings all cite real
// files has no Tier-4-eligible finding, so no index is ever constructed and the
// wazero runtime is never reached.
func TestTier4_NotConsultedForValidPaths(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/auth/session.go",
		Problem: "`RefreshToken` is fine but slow",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.True(t, findings[0].PathValid)
	assert.Empty(t, unresolved)
	assert.Zero(t, *built, "AC5: no Tier 4 index is constructed when nothing is eligible")
	assert.Zero(t, fake.calls)
}

// TestTier4_DisabledByEnv is AC6: the AST-grouping opt-out disables Tier 4 the
// same way it disables clustering — degrading to 5.4's Tier 1-3-only behavior,
// not erroring, and routing nothing to the sidecar (with no Tier 4 there is no
// evidence a finding is fabricated).
func TestTier4_DisabledByEnv(t *testing.T) {
	t.Setenv(astGroupingDisabledEnv, "1")

	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{byAnchor: map[string]string{"RefreshToken": "internal/auth/session.go"}}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "`RefreshToken` never checks the expiry",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.False(t, findings[0].PathValid, "Tier 1-3 existence validation still runs")
	assert.Equal(t, stream.PathNotFoundWarning, findings[0].PathWarning)
	assert.Empty(t, findings[0].PathSuggestion)
	assert.Empty(t, unresolved, "nothing is sidecar-routed when Tier 4 is off")
	assert.Zero(t, *built)
}

// TestTier4_NoAnchorsIsNeverSidecarEligible pins the clarified distinction: a
// finding whose prose names no identifier could not be checked, which is not the
// same as having been checked and found nothing.
func TestTier4_NoAnchorsIsNeverSidecarEligible(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{}
	withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "this file is too long and hard to follow",
		Fix:     "split it up",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.Empty(t, unresolved)
	assert.Empty(t, findings[0].PathSuggestion)
}

// TestTier4_DegradesWithoutGitIndex pins that a non-git root (nil candidate
// index) keeps 5.0 existence-only behavior and never reaches Tier 4: without a
// tracked file set there is no tree to have searched.
func TestTier4_DegradesWithoutGitIndex(t *testing.T) {
	root := t.TempDir() // no git repo here
	require.NoError(t, os.WriteFile(filepath.Join(root, "real.go"), []byte("package x\n"), 0o644))

	fake := &fakeTier4{}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "`quantumFlux` leaks a handle",
	}}
	unresolved := validateFindingPaths(context.Background(), findings, root)

	assert.Equal(t, stream.PathNotFoundWarning, findings[0].PathWarning)
	assert.Empty(t, unresolved)
	assert.Zero(t, *built)
}
