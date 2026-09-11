package fanout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
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
