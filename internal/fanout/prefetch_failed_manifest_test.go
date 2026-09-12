package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PrefetchStatus.Failed is the single distinction the type exists to make: a
// lookup that BROKE reads identically to one that cleanly matched nothing in
// every artifact otherwise. Both ship empty context; only one is a malfunction.
//
// TestManifest_PrefetchStatusDistinguishesDisabledFromNoMatch appears to cover
// it and does not. All three of its subtests are non-failing runs — a matched
// run, a max_prefetch_bytes: 0 run, and a clean no-match — and the field is
// `json:"failed,omitempty"`, so `assert.Nil(pf["failed"])` is satisfied by the
// FIXTURES rather than by anything the code does. Its header comment claims it
// covers a run "whose git grep broke"; no subtest breaks one.
//
// What this pins, and what it does not:
//
//	pinned      — a Failed status reaches status.json as `"failed": true` and
//	              survives a write/read round trip. A regression that dropped the
//	              field or its JSON tag fails here.
//	NOT pinned  — the end-to-end path from a genuinely broken `git grep` to this
//	              record. That is not constructible: BuildEntries calls prefetch()
//	              only AFTER buildEntriesValidated succeeds, so the changed-files
//	              and range-chunk failure arms cannot fire through the builder
//	              (their memo has already succeeded), and the one prefetch-only
//	              arm, referenceHits' lookupFailed, needs `git grep` to exit
//	              non-1 against a VALID head. The payload layer pins that arm
//	              directly instead, by calling buildPrefetch with an unresolvable
//	              rev (prefetch_test.go, "a broken lookup is recorded as failed,
//	              not absent").
func TestManifest_PrefetchFailedSurvivesPersistence(t *testing.T) {
	dir := t.TempDir()

	m := &payload.Manifest{
		Base:     "aaaa",
		Head:     "bbbb",
		Prefetch: &payload.PrefetchStatus{Failed: true},
	}
	require.NoError(t, WriteManifest(dir, m))

	t.Run("the decoded manifest still reports the failure", func(t *testing.T) {
		back, err := ReadManifest(dir)
		require.NoError(t, err)
		require.NotNil(t, back.Prefetch, "a recorded pre-fetch outcome must survive the round trip")

		assert.True(t, back.Prefetch.Failed,
			"a broken lookup must still read as broken after persistence; losing it here makes a malfunction indistinguishable from a repository with no consumers")
		assert.False(t, back.Prefetch.Present,
			"a failed lookup retrieved nothing, so Present must stay false")
		assert.False(t, back.Prefetch.Disabled,
			"'the lookup broke' must never read as 'the operator turned it off' — those call for opposite actions")
	})

	t.Run("the on-disk key is present and true", func(t *testing.T) {
		// Asserted against the raw JSON, not only the decoded struct: `failed` is
		// omitempty, so a regression that stopped SETTING it would round-trip
		// cleanly through the struct while vanishing from the file every external
		// reader actually parses.
		b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(b, &raw))

		pf, ok := raw["prefetch"].(map[string]any)
		require.True(t, ok, "the prefetch object must be written")
		assert.Equal(t, true, pf["failed"],
			"status.json must carry failed:true — this is the key an operator greps for when a review ships with no retrieved context")
	})
}
