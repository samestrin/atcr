package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	atcrlog "github.com/samestrin/atcr/internal/log"
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

func TestScopePrefetchGrounding_StripsWhenAFallbackShedTheContextBlock(t *testing.T) {
	// Grounding is computed ONCE for the whole review and shared by every agent,
	// but the Context Definitions block is a per-agent SHEDABLE entry. An agent
	// dispatched without it must not have findings on merely-referenced files
	// accepted by the Epic 14.1 gate — that is the fabricated-file class the gate
	// exists to stop, re-opened for every file the pre-fetch pass merely named.
	//
	// This fixture is specifically the one a primary-only check cannot catch: the
	// PRIMARY keeps the block while its FALLBACK sheds it to fund the ledger.
	var consumer strings.Builder
	consumer.WriteString("package p\n\nfunc Reconcile() string {\n")
	for i := 0; i < 34; i++ {
		consumer.WriteString("\t_ = " + itoa(i) + " // " + strings.Repeat("z", 30) + "\n")
	}
	consumer.WriteString("\treturn ReadStore() + WriteStore() + CloseStore()\n}\n")

	dir, base := seedClaimHeavyRepo(t,
		fixtureFile{"a.go", prefetchBandFile("ReadStore", "int", 2)},
		fixtureFile{"b.go", prefetchBandFile("WriteStore", "int", 76)},
		fixtureFile{"c.go", prefetchBandFile("CloseStore", "int", 76)},
		fixtureFile{"consumer.go", []byte(consumer.String())},
	)
	head := commitClaimHeavyHead(t, dir,
		fixtureFile{"a.go", prefetchBandFile("ReadStore", "string", 2)},
		fixtureFile{"b.go", prefetchBandFile("WriteStore", "string", 76)},
		fixtureFile{"c.go", prefetchBandFile("CloseStore", "string", 76)},
	)

	cfg := sizingRosterConfig()
	win := 14774
	g := cfg.Registry.Agents["greta"]
	g.ContextWindowTokens = &win
	cfg.Registry.Agents["greta"] = g
	kai := cfg.Registry.Agents["kai"]
	kai.Fallback = "greta"
	cfg.Registry.Agents["kai"] = kai
	cfg.Project.Agents = []string{"kai"}
	cfg.Settings.OnOverflow = OverflowTruncate

	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	var slots []Slot
	captureStderr(t, func() {
		slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
	})
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	require.NotEmpty(t, slots[0].Fallbacks, "PRECONDITION: kai must resolve its greta fallback")

	primaryKept := false
	for _, e := range slots[0].entries {
		if e.Path == payload.PrefetchContextPath {
			primaryKept = true
		}
	}
	require.True(t, primaryKept,
		"PRECONDITION: the PRIMARY must keep the block, or this fixture does not distinguish a primary-only check from a chain-wide one")

	fallbackShed := false
	for _, fb := range slots[0].Fallbacks {
		for _, p := range fb.Truncation.FilesDropped {
			if p == payload.PrefetchContextPath {
				fallbackShed = true
			}
		}
	}
	require.True(t, fallbackShed,
		"PRECONDITION: a fallback must have shed the block, or there is nothing for the guard to catch")

	changed := payload.ChangedLines{
		"a.go":        {Ranges: []payload.LineRange{{Start: 1, End: 2}}},
		"consumer.go": {Ranges: []payload.LineRange{{Start: 3, End: 9}}, PrefetchOnly: true},
	}

	got, _ := scopePrefetchGrounding(changed, slots)

	require.Contains(t, got, "a.go",
		"a genuinely changed file stays groundable — the guard governs retrieved spans only")
	require.NotContains(t, got, "consumer.go",
		"an agent in this chain was dispatched WITHOUT the Context Definitions block, so retrieved spans must not be groundable for this review")
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
		slots, _, err = buildSlots(context.Background(), cfg, payloads, ReviewRange{Base: base, Head: head}, "blocks", "", true, true)
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

// scopedPrefetchRepo is prefetchFanoutRepo plus an unrelated added file, so a
// mapper can keep one changed file and drop the other.
func scopedPrefetchRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir, base, _ = prefetchFanoutRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.go"), []byte("package p\n\nfunc Other() {}\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "add other")
	head = fanoutGit(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

func hasPrefetchEntry(mp modePayload) bool {
	for _, e := range mp.Entries {
		if e.Path == payload.PrefetchContextPath {
			return true
		}
	}
	return false
}

// A mapper that drops store.go must take the context retrieved for store.go's
// symbols with it; a mapper that fails open must leave pre-fetching on.
func TestBuildScopedPayloads_ThreatMapStripsPrefetchContext(t *testing.T) {
	dir, base, head := scopedPrefetchRepo(t)
	cfg := sizingRosterConfig()

	unscoped, _, _, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false, threatScope{})
	require.NoError(t, err)
	carries := false
	for _, mp := range unscoped {
		carries = carries || hasPrefetchEntry(mp)
	}
	require.True(t, carries, "PRECONDITION: the fixture must pre-fetch context without a threat map")

	scoped, _, rec, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false,
		threatScope{mapper: &fakeMapper{keep: []string{"other.go"}}})
	require.NoError(t, err)
	require.Equal(t, []string{"store.go"}, rec.Dropped)
	for mode, mp := range scoped {
		assert.Falsef(t, hasPrefetchEntry(mp), "mode %s: a mapper-dropped file's context must not reach a prompt", mode)
	}

	all, _, _, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false,
		threatScope{mapper: &fakeMapper{keep: []string{"store.go", "other.go"}}})
	require.NoError(t, err)
	carries = false
	for _, mp := range all {
		carries = carries || hasPrefetchEntry(mp)
	}
	assert.True(t, carries, "a keep-all map drops nothing, so the context stays")

	failed, _, _, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false,
		threatScope{mapper: &fakeMapper{err: assert.AnError}})
	require.NoError(t, err)
	carries = false
	for _, mp := range failed {
		carries = carries || hasPrefetchEntry(mp)
	}
	assert.True(t, carries, "a fail-open mapper keeps prefetch on")
}

// withoutPrefetchContext drops the Context Definitions entry and nothing else,
// keeping the order of what is left.
func TestWithoutPrefetchContext(t *testing.T) {
	ctxEntry := payload.FileEntry{Path: payload.PrefetchContextPath, Body: "context"}
	ledger := payload.FileEntry{Path: payload.ClaimLedgerPath, Body: "claims"}
	a := payload.FileEntry{Path: "a.go", Body: "a"}
	b := payload.FileEntry{Path: "b.go", Body: "b"}
	cases := []struct {
		name    string
		entries []payload.FileEntry
		want    []payload.FileEntry
	}{
		{"nil entries", nil, []payload.FileEntry{}},
		{"empty entries", []payload.FileEntry{}, []payload.FileEntry{}},
		{"no match", []payload.FileEntry{ledger, a, b}, []payload.FileEntry{ledger, a, b}},
		{"only the prefetch entry", []payload.FileEntry{ctxEntry}, []payload.FileEntry{}},
		{"prefetch among files", []payload.FileEntry{ledger, ctxEntry, a, b}, []payload.FileEntry{ledger, a, b}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := slices.Clone(tc.entries)
			got := withoutPrefetchContext(tc.entries)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, in, tc.entries, "the input must not be modified")
		})
	}
}

// claimLedgerBody returns the claim-ledger body a mode payload carries, or ""
// when it carries none.
func claimLedgerBody(mp modePayload) string {
	for _, e := range mp.Entries {
		if e.Path == payload.ClaimLedgerPath {
			return e.Body
		}
	}
	return ""
}

// The claim ledger is shed-exempt, so TrimToPaths keeps it whole. A mapper
// that drops store.go must take the claims of the commit that only touched
// store.go with it: no reviewer is sent that file, so every one of them would
// rule those claims UNSUPPORTED. The claims of a commit that touched a kept
// file stay, and a keep-all map or a fail-open mapper leaves the ledger whole.
func TestBuildScopedPayloads_ThreatMapScopesTheClaimLedger(t *testing.T) {
	dir, base, head := scopedPrefetchRepo(t)
	cfg := sizingRosterConfig()
	const storeClaim, otherClaim = "ReadStore() now returns a string", "add other"

	assertLedger := func(t *testing.T, payloads map[string]modePayload, want, notWant []string) {
		t.Helper()
		require.NotEmpty(t, payloads)
		for mode, mp := range payloads {
			body := claimLedgerBody(mp)
			for _, c := range want {
				assert.Containsf(t, body, c, "mode %s: the ledger must keep %q", mode, c)
			}
			for _, c := range notWant {
				assert.NotContainsf(t, body, c, "mode %s: the ledger must drop %q", mode, c)
			}
		}
	}

	unscoped, rb, _, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false, threatScope{})
	require.NoError(t, err)
	assertLedger(t, unscoped, []string{storeClaim, otherClaim}, nil)
	whole := rb.ClaimLedgerStatus()
	require.True(t, whole.Present, "PRECONDITION: the fixture must carry a claim ledger")

	scoped, rb, rec, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false,
		threatScope{mapper: &fakeMapper{keep: []string{"other.go"}}})
	require.NoError(t, err)
	require.Equal(t, []string{"store.go"}, rec.Dropped)
	assertLedger(t, scoped, []string{otherClaim}, []string{storeClaim, "change the store return shape"})
	st := rb.ClaimLedgerStatus()
	assert.True(t, st.Present)
	assert.Less(t, st.Claims, whole.Claims, "the manifest must count the claims delivered, not the claims read")
	assert.Equal(t, whole.Claims-st.Claims, st.ThreatMapDropped, "the manifest must say how many claims the threat map removed")

	keepStore, _, _, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false,
		threatScope{mapper: &fakeMapper{keep: []string{"store.go"}}})
	require.NoError(t, err)
	assertLedger(t, keepStore, []string{storeClaim}, []string{otherClaim})

	all, rb, _, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false,
		threatScope{mapper: &fakeMapper{keep: []string{"store.go", "other.go"}}})
	require.NoError(t, err)
	assertLedger(t, all, []string{storeClaim, otherClaim}, nil)
	assert.Equal(t, whole, rb.ClaimLedgerStatus(), "a keep-all map drops nothing, so the ledger stays whole")

	failed, rb, _, err := buildScopedPayloads(context.Background(), cfg, dir, base, head, false,
		threatScope{mapper: &fakeMapper{err: assert.AnError}})
	require.NoError(t, err)
	assertLedger(t, failed, []string{storeClaim, otherClaim}, nil)
	assert.Equal(t, whole, rb.ClaimLedgerStatus(), "a fail-open mapper leaves the ledger whole")
}

// When the threat map drops a file, buildScopedPayloads strips the Context
// Definitions block from every mode on purpose. The manifest must then stop
// claiming a delivered block, name the threat map as the cause, and the run
// must not warn that a budget shed revoked grounding. Grounding on the
// retrieved spans stays withdrawn, since no agent received them. A keep-all map
// and a fail-open mapper strip nothing, so both keep the ordinary record.
func TestPrepareReview_ThreatMapStripIsRecordedNotReportedAsAShed(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	readPrefetch := func(t *testing.T, dir string) map[string]any {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(b, &m))
		pf, ok := m["prefetch"].(map[string]any)
		require.True(t, ok, "a git-range review must record prefetch in its manifest")
		return pf
	}
	prepareWith := func(t *testing.T, cfg *ReviewConfig, mapper ThreatMapper) (*PreparedReview, string, ReviewRequest) {
		t.Helper()
		repo, base, head := scopedPrefetchRepo(t)
		req := reviewReq(repo, repo, base, head)
		req.OutputDir = filepath.Join(t.TempDir(), "review")
		req.ThreatMapper = mapper
		var logBuf bytes.Buffer
		ctx := atcrlog.NewContext(context.Background(),
			slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		prep, err := PrepareReview(ctx, cfg, req)
		require.NoError(t, err)
		return prep, logBuf.String(), req
	}
	prepare := func(t *testing.T, mapper ThreatMapper) (*PreparedReview, string, ReviewRequest) {
		t.Helper()
		return prepareWith(t, twoAgentConfig("http://unused"), mapper)
	}
	prefetchOnly := func(c payload.ChangedLines) int {
		n := 0
		for _, fc := range c {
			if fc.PrefetchOnly {
				n++
			}
		}
		return n
	}

	t.Run("a mapper drop records the strip and does not warn of a shed", func(t *testing.T) {
		prep, logs, req := prepare(t, &fakeMapper{keep: []string{"other.go"}})
		require.Equal(t, []string{"store.go"}, prep.threatMapDropped, "PRECONDITION: the mapper must drop a file, or nothing is stripped")

		pf := readPrefetch(t, prep.Dir)
		assert.Equal(t, false, pf["present"], "no agent received the block, so the manifest must not claim it")
		assert.Equal(t, true, pf["dropped_by_threat_map"], "the manifest must name the threat map as the cause")
		assert.Nil(t, pf["grounding_revoked"], "grounding_revoked means delivered then withdrawn; this block was never delivered")
		assert.NotContains(t, logs, "prefetch grounding revoked", "a deliberate strip is not a budget shed")
		assert.Contains(t, logs, "prefetch context stripped by the threat map", "the strip must still be traceable in the log")
		assert.Zero(t, prefetchOnly(prep.Changed), "grounding on retrieved spans stays withdrawn: no block was delivered")

		// The resume re-applies the recorded keep set, so it strips again and
		// must record the same cause, not a revocation.
		var logBuf bytes.Buffer
		ctx := atcrlog.NewContext(context.Background(),
			slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		rprep, _, err := PrepareResume(ctx, twoAgentConfig("http://unused"), prep.Dir, req)
		require.NoError(t, err)
		require.NotNil(t, rprep.prefetch)
		assert.False(t, rprep.prefetch.Present)
		assert.True(t, rprep.prefetch.DroppedByThreatMap)
		assert.False(t, rprep.prefetch.GroundingRevoked)
		assert.NotContains(t, logBuf.String(), "prefetch grounding revoked")
		assert.Zero(t, prefetchOnly(rprep.Changed))
	})

	t.Run("a mapper drop with no block retrieved records no strip", func(t *testing.T) {
		// max_prefetch_bytes: 0 means no block ever existed, so there was nothing
		// for the threat map to strip. Recording a strip here would be the same
		// false claim this test exists to prevent, in the other direction.
		cfg := twoAgentConfig("http://unused")
		off := int64(0)
		cfg.Settings.MaxPrefetchBytes = &off
		prep, logs, _ := prepareWith(t, cfg, &fakeMapper{keep: []string{"other.go"}})
		require.Equal(t, []string{"store.go"}, prep.threatMapDropped, "PRECONDITION: the mapper must drop a file, or the strip condition never holds")

		pf := readPrefetch(t, prep.Dir)
		assert.Equal(t, false, pf["present"])
		assert.Equal(t, true, pf["disabled"], "PRECONDITION: pre-fetching must be off, so no block was built")
		assert.Nil(t, pf["dropped_by_threat_map"], "no block existed, so the threat map stripped nothing")
		assert.NotContains(t, logs, "prefetch context stripped by the threat map")
	})

	t.Run("a keep-all map leaves the record unchanged", func(t *testing.T) {
		prep, logs, _ := prepare(t, &fakeMapper{keep: []string{"store.go", "other.go"}})
		pf := readPrefetch(t, prep.Dir)
		assert.Equal(t, true, pf["present"])
		assert.Nil(t, pf["dropped_by_threat_map"])
		assert.NotContains(t, logs, "prefetch grounding revoked")
		assert.NotZero(t, prefetchOnly(prep.Changed), "every agent kept the block, so its spans still ground")
	})

	t.Run("a fail-open mapper leaves the record unchanged", func(t *testing.T) {
		prep, logs, _ := prepare(t, &fakeMapper{err: assert.AnError})
		pf := readPrefetch(t, prep.Dir)
		assert.Equal(t, true, pf["present"])
		assert.Nil(t, pf["dropped_by_threat_map"])
		assert.NotContains(t, logs, "prefetch grounding revoked")
		assert.NotZero(t, prefetchOnly(prep.Changed))
	})
}
