package fanout

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	atcrlog "github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resumeShedFixture builds the revoking shape with the fallback's context window
// as a PARAMETER, so one fixture can produce both legs of the resume contract: a
// window wide enough that the fallback keeps the Context Definitions block (no
// revocation), and the narrow window revokedShedFixture hardcodes, where the
// fallback sheds it to fund the ledger (revocation).
//
// The chain is the same one TestManifest_PrefetchGroundingRevocationIsRecorded
// uses: the PRIMARY keeps the block while its FALLBACK sheds it, which is the one
// shape a primary-only check cannot distinguish.
func resumeShedFixture(t *testing.T, fallbackWindow int) (cfg *ReviewConfig, repo, base, head string) {
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
	g := cfg.Registry.Agents["greta"]
	g.ContextWindowTokens = &fallbackWindow
	cfg.Registry.Agents["greta"] = g
	kai := cfg.Registry.Agents["kai"]
	kai.Fallback = "greta"
	cfg.Registry.Agents["kai"] = kai
	cfg.Project.Agents = []string{"kai"}
	cfg.Settings.OnOverflow = OverflowTruncate

	return cfg, repo, base, head
}

// The resume leg re-scopes prefetch grounding against the PENDING slots and can
// revoke on a run whose interrupted predecessor did not — PrepareResume rebuilds
// the payloads against a freshly loaded config, so an operator who narrows a
// fallback's context window between the two runs gets a genuinely different
// outcome. Nothing carried that fact until resume.go gained its own revocation
// record, and both halves of it shipped with zero coverage: the warning block
// (resume.go:450-455) and the status stamp (resume.go:462-464) each read count 0
// against a fresh count-mode profile for this package.
//
// The fixture deliberately makes the FRESH leg NOT revoke. Asserting
// grounding_revoked:true after a resume whose interrupted run had already
// recorded it would pass with the stamp deleted — ExecuteResume copies the old
// manifest verbatim (m := *p.manifest), so the field would survive on the
// interrupted run's record and the assertion would be satisfied by the fixture
// rather than by the code. Starting from an unrevoked manifest is what makes the
// false→true transition observable, and it is the same lever the sibling test
// TestExecuteResume_PrefetchReflectsTheResumedRun pulls with max_prefetch_bytes.
func TestExecuteResume_PrefetchGroundingRevocationIsRecordedOnTheResumeLeg(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")

	// A window wide enough that the fallback keeps the block: no revocation.
	cfg, repo, base, head := resumeShedFixture(t, 200000)
	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "review")

	var prep *PreparedReview
	var err error
	captureStderr(t, func() {
		prep, err = PrepareReview(context.Background(), cfg, req)
	})
	require.NoError(t, err)
	require.NotNil(t, prep)

	before, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	require.NotNil(t, before.Prefetch, "PRECONDITION: a git-range review records the pre-fetch outcome")
	require.True(t, before.Prefetch.Present,
		"PRECONDITION: this fixture must retrieve a consumer, or the revocation below has nothing to revoke")
	require.False(t, before.Prefetch.GroundingRevoked,
		"PRECONDITION: the interrupted run must NOT be revoked, or the post-resume assertion passes on the carried-over record instead of the resume's own")

	// The operator narrows the fallback's window between the runs, so the resumed
	// chain sheds the block that the interrupted one kept. The config is derived
	// from cfg rather than rebuilt: PrepareResume locks the roster against the
	// manifest, so an independently-built config would fail ValidateResumeRoster
	// for reasons unrelated to what this test pins.
	cfg2 := narrowFallbackWindow(cfg, 14774)

	var logBuf bytes.Buffer
	ctx := atcrlog.NewContext(context.Background(),
		slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	// No agent completed, so kai is pending and the resume dispatches it.
	rprep, info, err := PrepareResume(ctx, cfg2, prep.Dir, req)
	require.NoError(t, err)
	require.False(t, info.AllComplete(),
		"PRECONDITION: an already-complete resume dispatches nobody and would revoke for a different reason")
	require.Len(t, rprep.Slots, 1, "PRECONDITION: kai is the only pending agent")

	// resume.go:450-455 — the operator-facing trace. log.FromContext returns a
	// discard logger without one in ctx, so this is asserted against a real
	// handler rather than captured stderr, which would pass vacuously.
	assert.Contains(t, logBuf.String(), "prefetch grounding revoked",
		"the resume leg must warn when it revokes; logging only on the fresh path leaves the leg most likely to be re-run with no trace")
	assert.Contains(t, logBuf.String(), "kai",
		"the warning must name the uncovered slot, or the reader must audit every slot by hand")

	_, err = ExecuteResume(ctx, okCompleter{}, rprep)
	require.NoError(t, err)

	// resume.go:462-464 — the stamp reaches disk.
	after, err := ReadManifest(prep.Dir)
	require.NoError(t, err)
	require.NotNil(t, after.Prefetch, "a range resume still has a range to report on")
	assert.True(t, after.Prefetch.GroundingRevoked,
		"the finalized manifest must record the revocation the RESUMED run performed; without it 'delivered and groundable' and 'delivered but revoked' stay byte-identical on the resume path")
	assert.True(t, after.Prefetch.Present,
		"the block WAS delivered — revocation is about groundability, not delivery")
}

// narrowFallbackWindow returns a copy of cfg whose fallback agent carries a
// narrower context window, leaving the roster and every other setting identical.
// The roster must not move: PrepareResume locks it against the manifest, so a
// second independently-built config would fail ValidateResumeRoster for reasons
// unrelated to what this test pins.
// The agent map is COPIED rather than mutated in place. ReviewConfig.Registry is
// a pointer, so assigning into cfg.Registry.Agents would narrow the window on the
// caller's config too — leaving the "interrupted run" and the "resumed run"
// sharing one roster, and the unrevoked precondition above satisfied by a config
// that had already been changed.
func narrowFallbackWindow(cfg *ReviewConfig, window int) *ReviewConfig {
	agents := make(map[string]registry.AgentConfig, len(cfg.Registry.Agents))
	for name, a := range cfg.Registry.Agents {
		agents[name] = a
	}
	g := agents["greta"]
	g.ContextWindowTokens = &window
	agents["greta"] = g

	reg := *cfg.Registry
	reg.Agents = agents
	out := *cfg
	out.Registry = &reg
	return &out
}
