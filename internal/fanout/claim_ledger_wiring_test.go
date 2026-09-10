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
// so the freeze holds by that reading, not by an untouched tree. The ledger
// contract's end-to-end assertions run behind internal/payload's exported wiring
// seam (VerifyClaimLedgerWiring / ClaimLedgerPromptSection), so the payload
// contract is proved by payload's own API; what remains here is the fanout-side
// plumbing those assertions need — roster and slot construction, the shed
// fixtures, and the fallback/refit levers — which is package-private to
// internal/fanout by construction.

// extractLedger moved behind internal/payload's exported wiring seam
// (ClaimLedgerPromptSection): the block's framing belongs to the package that
// renders it.

// AC1 + AC3 end to end: the ledger built in internal/payload survives every
// shed between buildPayloads and a rendered prompt, and every agent in the
// fan-out receives byte-identical claim text. The contract assertions run
// behind payload's wiring seam.
func TestClaimLedger_ReachesEveryAgentsRenderedPrompt(t *testing.T) {
	dir, base, head := fanoutRepo(t)

	cfg := sizingRosterConfig() // two agents, deliberately different context windows
	payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
	require.NoError(t, err)

	rng := ReviewRange{Base: base, Head: head}
	slots, _, err := buildSlots(cfg, payloads, rng, "", "", false)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(slots), 2, "precondition: the roster must fan out to more than one agent")

	first, ok := payload.ClaimLedgerPromptSection(slots[0].Primary.Prompt)
	require.True(t, ok, "prompt carries no claim ledger")
	assert.Contains(t, first, "Begin() no longer returns a wiped offset")
	assert.Contains(t, first, "Drain() keeps the previous offset")
	assert.Contains(t, first, "UNSUPPORTED")

	prompts := make(map[string]string, len(slots))
	for _, s := range slots {
		prompts[s.Primary.Name] = s.Primary.Prompt
	}
	require.NoError(t, payload.VerifyClaimLedgerWiring(prompts),
		"the ledger must reach every agent's rendered prompt, byte-identical across the fan-out")
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
		section, ok := payload.ClaimLedgerPromptSection(s.Primary.Prompt)
		require.True(t, ok, "prompt carries no claim ledger")
		assert.Contains(t, section, "Begin() no longer returns a wiped offset")
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

	for _, fb := range s.Fallbacks {
		require.NotEqual(t, s.Primary.Prompt, fb.Prompt,
			"precondition: the fallback must have re-fit (re-rendered) rather than inheriting the primary's prompt")
		require.True(t, fb.Truncation.Truncated,
			"precondition: the re-fit must actually shed a file, or this test proves nothing about the re-fit path")
		assert.NotContains(t, fb.Truncation.FilesDropped, payload.ClaimLedgerPath,
			"the ledger fits the fallback's budget: the exemption must keep it out of the re-fit's shed record")
		require.NoError(t, payload.VerifyClaimLedgerWiring(map[string]string{
			"primary":  s.Primary.Prompt,
			"fallback": fb.Prompt,
		}), "the re-fit fallback must carry a byte-identical claim ledger")
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
	primary, ok := payload.ClaimLedgerPromptSection(s.Primary.Prompt)
	require.True(t, ok, "precondition: the primary prompt carries no claim ledger")
	fbLedger, ok := payload.ClaimLedgerPromptSection(fb.Prompt)
	require.True(t, ok, "the sole kept entry must be the ledger")
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
		_, ok := payload.ClaimLedgerPromptSection(s.Primary.Prompt)
		assert.False(t, ok, "a branch that asserts nothing must render no claim ledger")
		assert.NotContains(t, s.Primary.Prompt, payload.ClaimLedgerPath)
	}
}

// keepSmallestEntry's Truncation.Truncated answers "was REVIEWABLE content
// dropped", not "did the slice shrink". On the range path a single-changed-file
// slot now carries two entries (the shed-exempt ledger + the file), so a count
// over ALL entries voids the invariant refitFallbackPayload documents: a slot
// with nothing left to shed must report Truncated=false, which is what makes the
// !trunc.Truncated arm decline the re-fit and keep the honest overflow record.
//
// FilesDropped is deliberately NOT filtered. A ledger that really was dropped
// still names itself there — accepted consequence #4 in
// internal/payload/claims.go ("the sentinel can reach a published artifact").
// The two fields answer different questions, and only Truncated was wrong.
func TestKeepSmallestEntry_TheLedgerIsNotCountedAsReviewableContent(t *testing.T) {
	ledger, file := ledgerAndOneFileEntry(t)

	t.Run("dropping only the ledger is not a truncation", func(t *testing.T) {
		small := file
		small.Body = "small\n" // smaller than the ledger, so the FILE is what survives
		kept, trunc, ok := keepSmallestEntry([]payload.FileEntry{ledger, small})
		require.True(t, ok)
		require.Equal(t, small.Path, kept[0].Path, "precondition: the file is the kept entry")

		assert.False(t, trunc.Truncated,
			"no reviewable file was dropped; Truncated=true here makes refitFallbackPayload re-fit a slot it documents it will decline")
		assert.Equal(t, []string{payload.ClaimLedgerPath}, trunc.FilesDropped,
			"the shed record still names the ledger it dropped — accepted effect #4, unchanged by this fix")
	})

	t.Run("dropping a real file still is a truncation", func(t *testing.T) {
		big := file
		big.Body = strings.Repeat("x\n", 10000) // larger than the ledger, so the LEDGER survives
		kept, trunc, ok := keepSmallestEntry([]payload.FileEntry{ledger, big})
		require.True(t, ok)
		require.Equal(t, payload.ClaimLedgerPath, kept[0].Path, "precondition: the ledger is the kept entry")

		assert.True(t, trunc.Truncated,
			"a reviewable file WAS dropped; reading this as 'nothing smaller to send' would strand the honest record")
		assert.Equal(t, []string{big.Path}, trunc.FilesDropped)
	})
}

// ledgerAndOneFileEntry returns a REAL claim-ledger entry plus one ordinary file
// entry from the same built payload. The ledger has to come from a real build:
// internal/payload's shedExempt sentinel is unexported precisely so nothing
// outside that package — including this test — can forge one.
func ledgerAndOneFileEntry(t *testing.T) (ledger, file payload.FileEntry) {
	t.Helper()
	dir, base, head := fanoutRepo(t)
	payloads, _, err := buildPayloads(context.Background(), sizingRosterConfig(), dir, base, head, false)
	require.NoError(t, err)

	for _, mp := range payloads {
		var l, f payload.FileEntry
		for _, e := range mp.Entries {
			if e.Path == payload.ClaimLedgerPath {
				l = e
			} else if f.Path == "" {
				f = e
			}
		}
		if l.Path != "" && f.Path != "" {
			return l, f
		}
	}
	t.Fatal("precondition: no built payload carried both a claim ledger and a file entry")
	return
}

// refitEntryBytes reports the ledger's rendered byte count and the smallest
// reviewable file's, read off the payload actually built. Those two numbers
// decide which side of the exemption's bound a given fallback budget falls on,
// so the re-fit tests derive their band from them instead of hardcoding it: a
// change to a fixture then moves the band with it rather than silently leaving
// a test asserting one mechanism while exercising another.
//
// It assumes a SINGLE-mode roster, and asserts that rather than trusting it.
// The counts are read off one built payload and Go randomizes map iteration
// order, so a mixed-mode roster would return diff-mode or blocks-mode bytes at
// random and every band precondition derived from them would go flaky. Both
// callers narrow cfg.Project.Agents to one agent, so neededModes yields one
// mode; the check below turns that accident into an enforced precondition.
func refitEntryBytes(t *testing.T, payloads map[string]modePayload) (ledger, smallestFile int64) {
	t.Helper()
	require.Len(t, payloads, 1,
		"refitEntryBytes assumes a single-mode roster: with two modes the bytes it returns depend on map iteration order")
	for _, mp := range payloads {
		var l, smallest int64
		for _, e := range mp.Entries {
			if e.Path == payload.ClaimLedgerPath {
				l = int64(len(e.Body))
				continue
			}
			if b := int64(len(e.Body)); smallest == 0 || b < smallest {
				smallest = b
			}
		}
		if l > 0 && smallest > 0 {
			return l, smallest
		}
	}
	t.Fatal("precondition: no built payload carried both a claim ledger and a reviewable file")
	return
}

// claimHeavyRepoFiles is claimHeavyRepo with the two files sized by the caller:
// each one's rendered entry grows with lines.
//
// The size is a parameter because the fallback re-fit's behaviour turns on how
// the files compare to the ledger, and claimHeavyRepo's 222-byte files can only
// reach one of the two bands. The re-fit is gated on inheritedPayloadFits, which
// sums the primary's CodeContext — and the ledger is ABSENT from CodeContext
// (accepted consequence #5 in internal/payload/claims.go: the audit seam
// discards everything above the first diff marker). So the gate opens only below
// the reviewable files' combined bytes, and with 222-byte files no budget large
// enough to hold an 8 KiB ledger ever re-fits at all. Larger files raise the
// gate above the ledger and open the band where the budget exceeds the ledger
// and still cannot fund it plus one file.
func claimHeavyRepoFiles(t *testing.T, lines int) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	fanoutGit(t, dir, "init", "-q", "-b", "main")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package p\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "seed the two files")
	base = fanoutGit(t, dir, "rev-parse", "HEAD")

	big := func(fn string) []byte {
		var b strings.Builder
		b.WriteString("package p\n\nfunc " + fn + "() {\n")
		for i := 0; i < lines; i++ {
			b.WriteString("\t_ = " + itoa(i) + " // " + strings.Repeat("z", 20) + "\n")
		}
		b.WriteString("}\n")
		return []byte(b.String())
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), big("A"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), big("B"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	var msg strings.Builder
	msg.WriteString("a branch that asserts a great deal\n\n")
	for i := 0; i < 40; i++ {
		msg.WriteString("- claim " + itoa(i) + ": " + strings.Repeat("w", 120) + "\n")
	}
	fanoutGit(t, dir, "commit", "-q", "-m", msg.String())
	head = fanoutGit(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

// claimHeavyRepo inverts paddedClaimingRepo's proportions: two tiny files behind
// a commit message long enough to render a ~8 KiB ledger. That is the shape in
// which the ledger is the LARGEST entry, which is what it takes to drive it past
// a fallback's budget — the padded fixture's 55 KiB files can never do it.
func claimHeavyRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	fanoutGit(t, dir, "init", "-q", "-b", "main")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n\nfunc A() int { return 0 }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package p\n\nfunc B() int { return 0 }\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "seed the two files")
	base = fanoutGit(t, dir, "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n\nfunc A() int { return 1 }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package p\n\nfunc B() int { return 2 }\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	var msg strings.Builder
	msg.WriteString("a branch that asserts a great deal\n\n")
	for i := 0; i < 40; i++ {
		msg.WriteString("- claim " + itoa(i) + ": " + strings.Repeat("w", 120) + "\n")
	}
	fanoutGit(t, dir, "commit", "-q", "-m", msg.String())
	head = fanoutGit(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

// The THIRD exception to the ledger's per-agent identity, and the one the
// shipping docs did not name. On the ordinary path the ledger carries Size 0, so
// `shedExempt && clampSize(Size) <= budget` (internal/payload/budget.go) holds
// for every budget and the ledger is unconditionally kept. The fallback re-fit
// re-sizes every entry to len(Body) before shedding, and at that point the
// bounded exemption becomes a real comparison: a ledger larger than the
// fallback's budget sheds like any other entry.
//
// The result is the asymmetry AC3 otherwise forbids — the primary adjudicates
// the claims and its backup, reviewing the same persona over the same range,
// receives none. It is accepted (the bound is what stops the ledger from
// dropping every reviewable file to fund itself), but accepted is not the same
// as undocumented, and "Two exceptions are deliberate" was wrong in both
// docs/payload-modes.md and CHANGELOG.md while this was reachable.
func TestClaimLedger_RefitBelowTheLedgersBytesDropsIt(t *testing.T) {
	dir, base, head := claimHeavyRepo(t)

	cfg := sizingRosterConfig()
	// greta declares a window whose byte budget (392) sits above one 222-byte
	// file and below the 8111-byte ledger — the band in which the exemption's own
	// clampSize(Size) <= budget bound sheds the ledger while real code survives.
	//
	// The window is 12288 tokens above the budget's token cost because
	// EffectiveByteBudget reserves BOTH the 8192-token output cap and the
	// 4096-token prompt overhead before converting at 7/2 bytes per token:
	// (12400 - 8192 - 4096) * 7 / 2 = 392. The 6000 that stood here omitted that
	// reservation, so the real budget was 0, every entry shed, and the ledger was
	// lost through the AllDropped reroute instead — a different mechanism, which
	// the assertions below now separate rather than assume.
	small := 12400
	g := cfg.Registry.Agents["greta"]
	g.ContextWindowTokens = &small
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
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
	})
	require.NoError(t, err)
	require.NotEmpty(t, slots, "precondition: the roster must produce a slot")
	s := slots[0]
	require.NotEmpty(t, s.Fallbacks, "precondition: kai must resolve its greta fallback")

	_, primaryHasLedger := payload.ClaimLedgerPromptSection(s.Primary.Prompt)
	require.True(t, primaryHasLedger,
		"precondition: the primary's budget holds the ledger, so the asymmetry is the fallback's alone")

	ledgerBytes, smallestFile := refitEntryBytes(t, payloads)

	for _, fb := range s.Fallbacks {
		// fb.rePacked, NOT fb.Truncation.Truncated: fbTrunc is initialized from the
		// PRIMARY's truncation (internal/fanout/review.go:3121), so Truncated is
		// already true whenever the primary shed a file and no re-fit ran at all.
		// Only rePacked is set by the re-fit arm itself, so only rePacked can carry
		// the precondition this message states.
		require.True(t, fb.rePacked,
			"precondition: the fallback must actually have re-fit, or this proves nothing")
		// Pin the BAND, not just the symptom. THREE mechanisms can strip the
		// ledger on this path and they are not interchangeable, so a test that
		// only asserts "the ledger is gone" can silently start proving a
		// different one. This case is the exemption's own bound: too small to
		// hold the ledger, big enough that a reviewable file still fits.
		require.Positive(t, fb.EffectiveBudget,
			"precondition: a 0 budget sheds every entry and reroutes through keepSmallestEntry — a different mechanism")
		require.Less(t, fb.EffectiveBudget, ledgerBytes,
			"precondition: this band is a budget BELOW the ledger, where clampSize(Size) <= budget fails and the bound sheds it")
		require.GreaterOrEqual(t, fb.EffectiveBudget, smallestFile,
			"precondition: a reviewable file must still fit, or AllDropped trips and keepSmallestEntry does the work instead")

		// Exactly the ledger plus the one file that did not fit, which with three
		// entries means exactly one reviewable file survived. A longer list means
		// every file shed and the reroute is doing the work.
		require.Len(t, fb.Truncation.FilesDropped, 2,
			"the ledger and the one file that did not fit; a longer list means the fixture left the band")
		assert.Contains(t, fb.Truncation.FilesDropped, payload.ClaimLedgerPath,
			"a ledger larger than the fallback's budget sheds like any other entry")
		_, ok := payload.ClaimLedgerPromptSection(fb.Prompt)
		assert.False(t, ok,
			"the re-fit fallback reviews the same range with no claims to adjudicate — the third exception")
	}
}

// The half the shipping docs had backwards: a fallback whose budget EXCEEDS the
// ledger's bytes still loses it. The exemption's bound PASSES here (8111 <=
// 9492), so the ledger is kept and every reviewable file sheds to fund it —
// which is precisely what AllDropped means, and refitFallbackPayload reroutes to
// keepSmallestEntry. That keeps the smallest ENTRY, a ~5 KB file here, so the
// ledger is the entry that goes.
//
// Fitting the budget is therefore necessary but not sufficient. The fixture's
// files are deliberately SMALLER than the ledger: when every file is larger,
// the same branch keeps the LEDGER and sheds all the code instead. Both
// preconditions are asserted below rather than assumed, because the two
// outcomes come out of one branch and look alike from the outside.
func TestClaimLedger_RefitAboveTheLedgersBytesStillDropsIt(t *testing.T) {
	dir, base, head := claimHeavyRepoFiles(t, 150)

	cfg := sizingRosterConfig()
	// (15000 - 8192 output - 4096 overhead) * 7 / 2 = 9492: above the 8111-byte
	// ledger, below ledger + one 5169-byte file, and below the two files'
	// combined bytes so inheritedPayloadFits fails and the re-fit gate opens.
	small := 15000
	g := cfg.Registry.Agents["greta"]
	g.ContextWindowTokens = &small
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
		slots, _, err = buildSlots(cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
	})
	require.NoError(t, err)
	require.NotEmpty(t, slots, "precondition: the roster must produce a slot")
	s := slots[0]
	require.NotEmpty(t, s.Fallbacks, "precondition: kai must resolve its greta fallback")

	_, primaryHasLedger := payload.ClaimLedgerPromptSection(s.Primary.Prompt)
	require.True(t, primaryHasLedger,
		"precondition: the primary's budget holds the ledger, so the asymmetry is the fallback's alone")

	ledgerBytes, smallestFile := refitEntryBytes(t, payloads)

	for _, fb := range s.Fallbacks {
		require.True(t, fb.rePacked,
			"precondition: the fallback must actually have re-fit, or this proves nothing")
		require.GreaterOrEqual(t, fb.EffectiveBudget, ledgerBytes,
			"the whole point of this case: the budget EXCEEDS the ledger, so the exemption's clampSize(Size) <= budget bound PASSES and cannot be what sheds it")
		require.Less(t, fb.EffectiveBudget, ledgerBytes+smallestFile,
			"precondition: the budget must not fund the ledger AND a file, or nothing sheds at all")
		require.Less(t, smallestFile, ledgerBytes,
			"precondition: a reviewable file must be smaller than the ledger, or keepSmallestEntry keeps the LEDGER and sheds the code instead")

		_, ok := payload.ClaimLedgerPromptSection(fb.Prompt)
		assert.False(t, ok,
			"a budget larger than the ledger is not enough to keep it: every file shed to fund it, AllDropped tripped, and keepSmallestEntry kept a file instead")
		assert.Contains(t, fb.Truncation.FilesDropped, payload.ClaimLedgerPath,
			"the shed record must name the ledger the reviewer did not receive")
		require.Len(t, fb.Truncation.FilesDropped, 2,
			"the ledger plus the file keepSmallestEntry did not keep — one reviewable file must survive, or this is the empty-payload case instead")
	}
}
