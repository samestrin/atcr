package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scopePrefetchGrounding can strip every PrefetchOnly key from the review-wide
// grounding map, and until now it did so SILENTLY. The manifest was already
// written by then (finalizePreparedReview writes it before the grounding block),
// so status.json kept reporting `prefetch: {present: true, snippets: N}` while
// every finding on a merely-referenced file was dropped by isGrounded.
//
// "Delivered and groundable" and "delivered but revoked" were therefore
// byte-identical in every artifact — the exact condition PrefetchStatus's own
// doc comment says the type exists to prevent. The only trace was the generic
// per-agent "dropped N ungrounded finding(s)" line on stderr, which reads as
// ordinary hallucination filtering.
//
// The operator consequence is concrete: the Context Definitions block was paid
// for in provider bytes on every agent, and nothing on disk said the context it
// bought had been made unusable.

// readRevokedManifest returns the decoded manifest.json for a prepared review.
func readRevokedManifest(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

// revokedShedFixture builds the range and roster whose FALLBACK sheds the
// Context Definitions block while the PRIMARY keeps it — the shape that makes
// scopePrefetchGrounding revoke. It mirrors the fixture
// TestScopePrefetchGrounding_StripsWhenAFallbackShedTheContextBlock uses,
// because that is the one shape a primary-only check cannot distinguish.
func revokedShedFixture(t *testing.T) (cfg *ReviewConfig, repo, base, head string) {
	t.Helper()

	var consumer strings.Builder
	consumer.WriteString("package p\n\nfunc Reconcile() string {\n")
	for i := 0; i < 34; i++ {
		consumer.WriteString("\t_ = " + itoa(i) + " // " + strings.Repeat("z", 30) + "\n")
	}
	consumer.WriteString("\treturn ReadStore() + WriteStore() + CloseStore()\n}\n")

	repo, base = seedClaimHeavyRepo(t,
		fixtureFile{"a.go", prefetchBandFile("ReadStore", "int", 2)},
		fixtureFile{"b.go", prefetchBandFile("WriteStore", "int", 76)},
		fixtureFile{"c.go", prefetchBandFile("CloseStore", "int", 76)},
		fixtureFile{"consumer.go", []byte(consumer.String())},
	)
	head = commitClaimHeavyHead(t, repo,
		fixtureFile{"a.go", prefetchBandFile("ReadStore", "string", 2)},
		fixtureFile{"b.go", prefetchBandFile("WriteStore", "string", 76)},
		fixtureFile{"c.go", prefetchBandFile("CloseStore", "string", 76)},
	)

	cfg = sizingRosterConfig()
	win := 14774
	g := cfg.Registry.Agents["greta"]
	g.ContextWindowTokens = &win
	cfg.Registry.Agents["greta"] = g
	kai := cfg.Registry.Agents["kai"]
	kai.Fallback = "greta"
	cfg.Registry.Agents["kai"] = kai
	cfg.Project.Agents = []string{"kai"}
	cfg.Settings.OnOverflow = OverflowTruncate

	return cfg, repo, base, head
}

func TestManifest_PrefetchGroundingRevocationIsRecorded(t *testing.T) {
	cfg, repo, base, head := revokedShedFixture(t)

	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "review")

	var prep *PreparedReview
	var err error
	captureStderr(t, func() {
		prep, err = PrepareReview(context.Background(), cfg, req)
	})
	require.NoError(t, err)
	require.NotNil(t, prep)

	// PRECONDITION: the revocation must actually have happened, or the assertion
	// below would pass against a run that had nothing to record.
	for p, fc := range prep.Changed {
		require.False(t, fc.PrefetchOnly,
			"PRECONDITION: %s is still groundable, so this fixture did not revoke and proves nothing", p)
	}

	pf, ok := readRevokedManifest(t, prep.Dir)["prefetch"].(map[string]any)
	require.True(t, ok, "a git-range review must record prefetch in its manifest")

	assert.Equal(t, true, pf["present"],
		"the block WAS delivered — revocation is about grounding, not delivery")
	assert.Equal(t, true, pf["grounding_revoked"],
		"a review whose retrieved-span widening was revoked must be distinguishable in status.json from one where it applied; otherwise the operator paid for context no artifact admits was made unusable")
}

func TestManifest_PrefetchGroundingNotRevokedWhenEveryAgentKeptTheBlock(t *testing.T) {
	// The control. Without it the assertion above is satisfied by a field that is
	// simply always true, which would report every healthy run as revoked.
	repo, base, head := prefetchFanoutRepo(t)
	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "review")

	prep, err := PrepareReview(context.Background(), twoAgentConfig("http://unused"), req)
	require.NoError(t, err)
	require.NotNil(t, prep)

	prefetched := false
	for _, fc := range prep.Changed {
		if fc.PrefetchOnly {
			prefetched = true
			break
		}
	}
	require.True(t, prefetched,
		"PRECONDITION: this fixture must retain a retrieved span, or 'not revoked' is vacuous")

	pf, ok := readRevokedManifest(t, prep.Dir)["prefetch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, pf["present"])
	assert.Nil(t, pf["grounding_revoked"],
		"a run where every dispatched agent kept the block must not be marked revoked")
}

// TestScopePrefetchGrounding_ReportsWhetherItRevoked pins the signal at its
// source. The manifest tests above prove the field reaches disk; this one
// proves the helper itself distinguishes the two outcomes, so a future caller
// that forgets to thread the value fails here rather than silently recording
// false on every run.
func TestScopePrefetchGrounding_ReportsWhetherItRevoked(t *testing.T) {
	changed := payload.ChangedLines{
		"a.go":        {Ranges: []payload.LineRange{{Start: 1, End: 2}}},
		"consumer.go": {Ranges: []payload.LineRange{{Start: 3, End: 9}}, PrefetchOnly: true},
	}

	t.Run("an entry-less slot revokes and says so", func(t *testing.T) {
		got, revoked := scopePrefetchGrounding(changed, []Slot{{Primary: Agent{Name: "kai"}}})
		assert.True(t, revoked, "an entry-less slot is not evidence the block was delivered")
		assert.NotContains(t, got, "consumer.go")
		assert.Contains(t, got, "a.go", "a genuinely changed file is never affected")
	})

	t.Run("a slot that kept the block does not revoke", func(t *testing.T) {
		kept := []Slot{{
			Primary: Agent{Name: "kai"},
			entries: []payload.FileEntry{{Path: payload.PrefetchContextPath}},
		}}
		got, revoked := scopePrefetchGrounding(changed, kept)
		assert.False(t, revoked)
		assert.Contains(t, got, "consumer.go", "the widening survives when every agent got the block")
	})

	t.Run("a map with no retrieved span never reports a revocation", func(t *testing.T) {
		onlyChanged := payload.ChangedLines{"a.go": {Ranges: []payload.LineRange{{Start: 1, End: 2}}}}
		got, revoked := scopePrefetchGrounding(onlyChanged, nil)
		assert.False(t, revoked, "there was nothing to revoke — this must not read as a revocation")
		assert.Equal(t, onlyChanged, got)
	})
}
