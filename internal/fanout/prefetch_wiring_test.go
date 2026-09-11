package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Context-aware pre-fetching (Epic 35.16.8) is produced in internal/payload, but
// two halves of its contract can only be proved here: that max_prefetch_bytes
// actually REACHES the payload builder, and that a second synthetic entry does
// not corrupt the file count the manifest and every persona template report.
//
// A unit test inside internal/payload cannot cover either — the setting is
// resolved and threaded in this package, and FileCount is computed here.

// prefetchFanoutRepo builds a range whose consumer is committed at BASE and
// never touched again, so it is absent from the diff entirely and can reach a
// reviewer only by reference retrieval.
//
// fanoutRepo is deliberately not reused: it ADDS drain.go at head, so the only
// caller of the changed symbol is itself a changed file and is excluded as
// already-in-payload. Pre-fetching cannot fire on that shape, which would make
// every assertion below pass vacuously.
func prefetchFanoutRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	fanoutGit(t, dir, "init", "-q", "-b", "main")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "store.go"),
		[]byte("package p\n\nfunc ReadStore(path string) ([]byte, error) {\n\treturn nil, nil\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "consumer.go"),
		[]byte("package p\n\nfunc Reconcile(path string) error {\n\t_, err := ReadStore(path)\n\treturn err\n}\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "seed the store and its consumer")
	base = fanoutGit(t, dir, "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "store.go"),
		[]byte("package p\n\nfunc ReadStore(path string) (string, error) {\n\treturn \"\", nil\n}\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "change the store return shape\n\n- ReadStore() now returns a string\n")
	head = fanoutGit(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

// prefetchEntryCount reports how many Context Definitions entries a mode payload
// carries.
func prefetchEntryCount(mp modePayload) int {
	n := 0
	for _, e := range mp.Kept {
		if e.Path == payload.PrefetchContextPath {
			n++
		}
	}
	return n
}

func TestPrefetch_MaxPrefetchBytesZeroDisablesEndToEnd(t *testing.T) {
	dir, base, head := prefetchFanoutRepo(t)
	cfg := sizingRosterConfig()

	// Precondition: with the setting unset the feature is ON by default, so this
	// fixture must actually produce a Context Definitions entry. Without it the
	// disabled-case assertion below could pass on a fixture where pre-fetching
	// never fires, proving nothing about the wiring.
	enabled, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)
	total := 0
	for _, mp := range enabled {
		total += prefetchEntryCount(mp)
	}
	require.NotZero(t, total,
		"PRECONDITION: this fixture must produce pre-fetched context by default, or the disable assertion is vacuous")

	// The operator sets max_prefetch_bytes: 0. Nothing retrieved from outside the
	// diff may reach the payload.
	zero := int64(0)
	cfg.Settings.MaxPrefetchBytes = &zero

	disabled, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)
	for mode, mp := range disabled {
		require.Zerof(t, prefetchEntryCount(mp),
			"mode %s: max_prefetch_bytes: 0 must disable pre-fetching end to end", mode)
	}
}

// PrefetchStatus exists so an absent section, a disabled feature and a failed
// lookup stay distinguishable in the persisted artifacts. The claim ledger's
// status reaches the manifest; this one must too — otherwise a run with
// max_prefetch_bytes: 0, one whose `git grep` broke, and one that simply
// matched nothing are byte-identical in status.json, which is exactly the
// condition the type's own doc comment says it exists to prevent.
func TestManifest_PrefetchStatusDistinguishesDisabledFromNoMatch(t *testing.T) {
	readManifest := func(t *testing.T, dir string) map[string]any {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(b, &m))
		return m
	}

	t.Run("a matched run records present with a snippet count", func(t *testing.T) {
		repo, base, head := prefetchFanoutRepo(t)
		req := reviewReq(repo, repo, base, head)
		req.OutputDir = filepath.Join(t.TempDir(), "review")
		prep, err := PrepareReview(context.Background(), twoAgentConfig("http://unused"), req)
		require.NoError(t, err)

		pf, ok := readManifest(t, prep.Dir)["prefetch"].(map[string]any)
		require.True(t, ok, "a git-range review must record prefetch in its manifest")
		assert.Equal(t, true, pf["present"])
		assert.Greater(t, pf["snippets"], float64(0), "this fixture retrieves a consumer")
		assert.Nil(t, pf["disabled"])
		assert.Nil(t, pf["failed"])
	})

	t.Run("a disabled run is recorded as disabled, not absent or failed", func(t *testing.T) {
		repo, base, head := prefetchFanoutRepo(t)
		cfg := twoAgentConfig("http://unused")
		zero := int64(0)
		cfg.Settings.MaxPrefetchBytes = &zero
		req := reviewReq(repo, repo, base, head)
		req.OutputDir = filepath.Join(t.TempDir(), "review")
		prep, err := PrepareReview(context.Background(), cfg, req)
		require.NoError(t, err)

		pf, ok := readManifest(t, prep.Dir)["prefetch"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, pf["disabled"])
		assert.Equal(t, false, pf["present"])
		assert.Nil(t, pf["failed"], "'you told us not to' must never read as 'we could not'")
	})

	t.Run("a no-match run is absent but not disabled or failed", func(t *testing.T) {
		// initRepo's change touches only one-letter symbol names, which
		// validGrepSymbol rejects as not worth a slot — the lookup never runs and
		// nothing is retrieved.
		repo, base, head := initRepo(t)
		req := reviewReq(repo, repo, base, head)
		req.OutputDir = filepath.Join(t.TempDir(), "review")
		prep, err := PrepareReview(context.Background(), twoAgentConfig("http://unused"), req)
		require.NoError(t, err)

		pf, ok := readManifest(t, prep.Dir)["prefetch"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, pf["present"])
		assert.Nil(t, pf["disabled"])
		assert.Nil(t, pf["failed"],
			"a clean no-match must not read as a broken lookup")
	})
}

func TestPrefetch_FileCountExcludesSyntheticEntries(t *testing.T) {
	// FileCount is carried into the manifest and into the persona-visible
	// {{.FileCount}}. It must report the files the RANGE changed, not the
	// engine-rendered sections prepended to them — with both the claim ledger and
	// the Context Definitions block present, len(kept) over-reports by two.
	dir, base, head := prefetchFanoutRepo(t)
	cfg := sizingRosterConfig()

	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	for mode, mp := range payloads {
		// Precondition, mirroring the disabled-end-to-end test above: the fixture
		// must actually produce exactly one of EACH synthetic section per mode.
		// Without this, a build that stopped emitting the ledger or the context
		// block would still pass the count comparison below while covering one
		// section or none — the exact vacuity the recomputed filter hides.
		ledger, contexts := 0, 0
		for _, e := range mp.Kept {
			switch e.Path {
			case payload.ClaimLedgerPath:
				ledger++
			case payload.PrefetchContextPath:
				contexts++
			}
		}
		require.Equalf(t, 1, ledger,
			"mode %s: PRECONDITION — fixture must produce exactly one claim ledger entry, or the FileCount comparison proves nothing about synthetic exclusion", mode)
		require.Equalf(t, 1, contexts,
			"mode %s: PRECONDITION — fixture must produce exactly one Context Definitions entry, or the FileCount comparison proves nothing about synthetic exclusion", mode)
		// The ABSOLUTE changed-file count of the fixture (prefetchFanoutRepo
		// changes exactly one file: store.go), asserted outright instead of
		// recomputed with the same path filter the production code uses — a
		// recomputation is tautological: it matches even if FileCount drifted to
		// count a section the filter happens to miss.
		require.Equalf(t, 1, mp.FileCount,
			"mode %s: FileCount must equal the fixture's one changed file, not a count inflated by synthetic engine-rendered sections", mode)
	}
}

func TestBuildSlots_PerAgentFileCountExcludesSyntheticEntries(t *testing.T) {
	// The per-agent re-shed in buildSlots re-derives the prompt's file count from
	// len(kept) — but kept retains the shed-exempt synthetic sections (claim
	// ledger + Context Definitions), so an agent whose payload carries them is
	// told "Reviewing 3 changed file(s)" for a range that changed ONE file, while
	// the manifest and the global build report the corrected count. The per-agent
	// re-derivation must apply the same ReviewableCount rule as the global build.
	repo, base, head := prefetchFanoutRepo(t)
	cfg := sizingRosterConfig()

	payloads, _, err := buildPayloads(context.Background(), cfg, repo, base, head, false)
	require.NoError(t, err)

	// Precondition, mirroring TestPrefetch_FileCountExcludesSyntheticEntries: the
	// fixture must carry exactly one of EACH synthetic section and report the
	// ABSOLUTE one-changed-file count globally — otherwise the per-agent
	// assertion below proves nothing about the re-shed divergence.
	mp, ok := payloads["blocks"]
	require.True(t, ok, "PRECONDITION: fixture must resolve the blocks mode")
	ledger, contexts := 0, 0
	for _, e := range mp.Entries {
		switch e.Path {
		case payload.ClaimLedgerPath:
			ledger++
		case payload.PrefetchContextPath:
			contexts++
		}
	}
	require.Equal(t, 1, ledger, "PRECONDITION: fixture must produce exactly one claim ledger entry")
	require.Equal(t, 1, contexts, "PRECONDITION: fixture must produce exactly one Context Definitions entry")
	require.Equal(t, 1, mp.FileCount, "PRECONDITION: the global build must report the fixture's one changed file")

	slot, _, err := buildOneAgent(cfg, "kai", payloads, ReviewRange{Base: base, Head: head}, "", "")
	require.NoError(t, err)
	require.Contains(t, slot.Prompt, "Reviewing 1 changed file(s)",
		"the per-agent prompt must report ReviewableCount(kept) — the same rule as the global build — not len(kept) over the synthetic-section-retaining survivor set")
}

func TestBuildSlots_ZeroBudgetArmFileCountExcludesSyntheticEntries(t *testing.T) {
	// The zero-budget bulk arm reports a HARDCODED count of 1 for whichever single
	// entry it keeps, and keepSmallestEntry picks by len(Body) with no shedExempt
	// awareness. So the entry it keeps can be a synthetic section, and the agent is
	// then told it is reviewing one changed file while having received no
	// reviewable content at all.
	//
	// Its two sibling arms — the re-pack below it and the fallback re-fit — both
	// already route through payload.ReviewableCount. This one did not.
	repo, base, head := prefetchFanoutRepo(t)

	payloads, _, err := buildPayloads(context.Background(), sizingRosterConfig(), repo, base, head, false)
	require.NoError(t, err)

	mp, ok := payloads["blocks"]
	require.True(t, ok, "PRECONDITION: fixture must resolve the blocks mode")

	// Inflate every REVIEWABLE entry so the smallest non-empty entry is certainly
	// a synthetic section. Mutated IN PLACE: shedExempt is unexported, so a
	// rebuilt FileEntry would silently lose the flag ReviewableCount keys on and
	// the assertion below would prove nothing.
	pad := strings.Repeat("x", 100000)
	for i := range mp.Entries {
		switch mp.Entries[i].Path {
		case payload.ClaimLedgerPath, payload.PrefetchContextPath:
			// leave the synthetic sections small
		default:
			mp.Entries[i].Body += pad
			mp.Entries[i].Size = int64(len(mp.Entries[i].Body))
		}
	}
	payloads["blocks"] = mp

	cfg := declaredWindowRoster(t, 1)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}

	var slots []Slot
	captureStderr(t, func() {
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: base, Head: head}, "blocks", "", true, true)
	})
	require.NoError(t, err)
	require.Len(t, slots, 1)

	p := slots[0].Primary
	require.Len(t, p.chunkFiles, 1, "PRECONDITION: the zero-budget arm keeps exactly one entry")
	require.Containsf(t, []string{payload.ClaimLedgerPath, payload.PrefetchContextPath}, p.chunkFiles[0],
		"PRECONDITION: the kept entry must be a synthetic section or the count proves nothing (kept %q)", p.chunkFiles[0])

	require.Contains(t, p.Prompt, "Reviewing 0 changed file(s)",
		"an agent handed only a synthetic section reviewed NO changed file; reporting 1 sends it hunting for content it never received")
}
