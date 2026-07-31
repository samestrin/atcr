package reconcile

import "testing"

// Epic 35.9.1: the consensus filter's corroboration bar is configurable via
// Options.Consensus — strict (default, epic-14.2 behavior), lenient (keep
// MEDIUM-confidence singletons), or off (filter inert). ONLY that bar moves:
// consensusMinReviewers, consensusExempt, and trustExempt behave identically at
// every level, and demoteByTrust runs ahead of the filter unaffected by it.

// levelPanel is a 3-reviewer panel in which every finding is an uncorroborated
// MEDIUM singleton with no exemption of any kind — the shape strict drops
// wholesale, so any survivor is attributable solely to the level.
func levelPanel() []Source {
	return []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "possible nil deref on this path", "correctness", "a"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "bar.go", 20, "unused import lingers in this file", "style", "b"),
		}},
		{Name: "c", Findings: []Finding{
			cf("LOW", "baz.go", 30, "this name reads ambiguously here", "style", "c"),
		}},
	}
}

// TestConsensusLevel_OffKeepsEverySingleton (AC1): off makes the filter inert —
// every singleton reaches Findings and ConsensusFiltered stays 0.
func TestConsensusLevel_OffKeepsEverySingleton(t *testing.T) {
	res := Reconcile(levelPanel(), Options{Consensus: ConsensusOff})

	isTrue(t, hasFinding(res, "foo.go"), "off keeps the MEDIUM singleton")
	isTrue(t, hasFinding(res, "bar.go"), "off keeps the second MEDIUM singleton")
	isTrue(t, hasFinding(res, "baz.go"), "off keeps the LOW singleton")
	eq(t, res.Summary.ConsensusFiltered, 0, "off leaves ConsensusFiltered at 0")
	isTrue(t, !inAmbiguousSingleton(res, "foo.go"), "off routes nothing to the consensus sidecar")
}

// sidecarPanel is a 3-reviewer panel that produces a genuine DBSCAN sidecar
// before the consensus filter ever runs, which levelPanel deliberately does not:
//
//   - noise.go:1 — "a" and "b" report identical text (they merge into a dense
//     cluster); "c" reports something unrelated at the same location, so DBSCAN
//     isolates it as a true noise singleton.
//   - gray.go:1 — "a" and "b" merge again; "c"'s text overlaps theirs at a
//     Jaccard inside the gray band [0.4, 0.7), so the pair is recorded as an
//     ambiguous gray pair rather than merged or isolated.
//
// Every finding is LOW/style so nothing is consensusExempt, and the two merged
// pairs are ConfHigh so the level cannot touch them — the only level-dependent
// entries in the sidecar are the consensus filter's own.
func sidecarPanel() []Source {
	return []Source{
		{Name: "a", Findings: []Finding{
			cf("LOW", "noise.go", 1, "token never expires unchecked here", "style", "a"),
			cf("LOW", "gray.go", 1, "alpha beta gamma delta epsilon", "style", "a"),
		}},
		{Name: "b", Findings: []Finding{
			cf("LOW", "noise.go", 1, "token never expires unchecked here", "style", "b"),
			cf("LOW", "gray.go", 1, "alpha beta gamma delta epsilon", "style", "b"),
		}},
		{Name: "c", Findings: []Finding{
			cf("LOW", "noise.go", 1, "completely different spurious claim", "style", "c"),
			cf("LOW", "gray.go", 1, "alpha beta gamma zeta eta", "style", "c"),
		}},
	}
}

// TestConsensusLevel_OffPreservesDBSCANSidecar (AC1) pins the half of the off
// contract that TestConsensusLevel_OffKeepsEverySingleton can only assert
// vacuously: off stops consensus ROUTING, it does not empty the sidecar. Every
// other fixture in this file yields zero DBSCAN output, so `!inAmbiguousSingleton`
// there passes against an empty slice and a mutation wiping the sidecar under off
// goes unnoticed. Here the sidecar is non-empty before the filter runs, and the
// DBSCAN portion is asserted byte-for-byte identical to the strict run's.
//
// (Verified by mutation: injecting `if !filterEnabled { ambiguous = nil }` into
// Reconcile fails this test on the gray-pair and noise assertions below.)
func TestConsensusLevel_OffPreservesDBSCANSidecar(t *testing.T) {
	strict := Reconcile(sidecarPanel(), Options{Consensus: ConsensusStrict})
	off := Reconcile(sidecarPanel(), Options{Consensus: ConsensusOff})

	// The consensus filter appends its own single-finding clusters AFTER every
	// dedupe cluster's entries, so off's whole sidecar is exactly strict's
	// DBSCAN-only prefix.
	dbscanLen := len(off.Ambiguous)
	isTrue(t, dbscanLen > 0, "the panel must produce a non-empty DBSCAN sidecar, or this test is vacuous")
	isTrue(t, len(strict.Ambiguous) > dbscanLen, "strict must additionally sidecar its filtered singletons")
	deepEq(t, off.Ambiguous, strict.Ambiguous[:dbscanLen],
		"off must leave the DBSCAN portion of the sidecar untouched")

	// Both DBSCAN shapes must actually be present — a gray pair (2 findings) and a
	// true noise singleton (1 finding) — so neither can be wiped unnoticed.
	grayPairs, noiseSingletons := 0, 0
	for _, c := range off.Ambiguous {
		switch len(c.Findings) {
		case 1:
			noiseSingletons++
		case 2:
			grayPairs++
		}
	}
	isTrue(t, grayPairs > 0, "off keeps DBSCAN gray pairs in the sidecar")
	isTrue(t, noiseSingletons > 0, "off keeps DBSCAN noise singletons in the sidecar")

	eq(t, off.Summary.NoiseCount, strict.Summary.NoiseCount, "NoiseCount is level-independent")
	isTrue(t, off.Summary.NoiseCount > 0, "the panel must produce real DBSCAN noise")
	eq(t, off.Summary.AmbiguousCount, dbscanLen, "AmbiguousCount counts the surviving sidecar")
	eq(t, off.Summary.AmbiguousHash, AmbiguousHash(strict.Ambiguous[:dbscanLen]),
		"off's sidecar digest is the strict run's DBSCAN-only digest")
	eq(t, off.Summary.ConsensusFiltered, 0, "off routes nothing to the sidecar itself")
}

// TestConsensusLevel_LenientKeepsMediumDropsLow (AC1): lenient raises the
// kept-bar to MEDIUM — a MEDIUM singleton survives, a LOW one is still
// sidecarred.
func TestConsensusLevel_LenientKeepsMediumDropsLow(t *testing.T) {
	// ConfidenceFor assigns MEDIUM to a single-reviewer finding regardless of
	// severity, so a LOW-severity singleton is still ConfMedium. Drive the
	// LOW-confidence case through the trust prior, the only reconcile-time path
	// to ConfLow (epic 35.9's demoteByTrust).
	res := Reconcile(levelPanel(), Options{
		Consensus:   ConsensusLenient,
		TrustPriors: map[string]float64{"c": 0.1}, // at/below trustLowThreshold
	})

	isTrue(t, hasFinding(res, "foo.go"), "lenient keeps a MEDIUM-confidence singleton")
	isTrue(t, hasFinding(res, "bar.go"), "lenient keeps the second MEDIUM singleton")
	isTrue(t, !hasFinding(res, "baz.go"), "lenient still sidecars a ConfLow singleton")
	isTrue(t, inAmbiguousSingleton(res, "baz.go"), "the dropped LOW singleton is recoverable from the sidecar")
	eq(t, res.Summary.ConsensusFiltered, 1, "exactly the ConfLow singleton is filtered")
}

// TestConsensusLevel_StrictMatchesDefault (AC1 golden): strict, the empty
// string, an uppercase token, and an unset Options field all reproduce the
// identical pre-change result — the regression guard on the default path.
func TestConsensusLevel_StrictMatchesDefault(t *testing.T) {
	baseline := Reconcile(levelPanel(), Options{})

	// Every singleton is dropped today; assert that explicitly so the comparison
	// below cannot pass by both sides being trivially empty in the wrong way.
	eq(t, baseline.Summary.ConsensusFiltered, 3, "strict sidecars all three singletons")
	length(t, baseline.Findings, 0, "no singleton survives strict")

	for _, level := range []string{ConsensusStrict, "", "STRICT", "  strict  "} {
		got := Reconcile(levelPanel(), Options{Consensus: level})
		deepEq(t, got.Findings, baseline.Findings, "Findings must match the default for level "+level)
		deepEq(t, got.Ambiguous, baseline.Ambiguous, "Ambiguous must match the default for level "+level)
		eq(t, got.Summary.ConsensusFiltered, baseline.Summary.ConsensusFiltered,
			"ConsensusFiltered must match the default for level "+level)
	}
}

// TestConsensusLevel_UnrecognizedFailsSafeToStrict: a level that somehow
// bypassed boundary validation must never silently disable the filter.
func TestConsensusLevel_UnrecognizedFailsSafeToStrict(t *testing.T) {
	res := Reconcile(levelPanel(), Options{Consensus: "bogus"})
	eq(t, res.Summary.ConsensusFiltered, 3, "an unrecognized level falls back to strict, not off")
}

// TestConsensusLevel_PanelFloorUnchangedAtEveryLevel (AC5): consensusMinReviewers
// gates the filter identically at every level — a 2-reviewer panel keeps its
// singletons under strict and lenient alike, and off changes nothing there.
func TestConsensusLevel_PanelFloorUnchangedAtEveryLevel(t *testing.T) {
	smallPanel := []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "possible nil deref on this path", "correctness", "a"),
		}},
		{Name: "b", Findings: []Finding{
			cf("LOW", "bar.go", 20, "unused import lingers in this file", "style", "b"),
		}},
	}
	for _, level := range []string{ConsensusStrict, ConsensusLenient, ConsensusOff} {
		res := Reconcile(smallPanel, Options{Consensus: level})
		isTrue(t, hasFinding(res, "foo.go"), "below the panel floor nothing is filtered at level "+level)
		isTrue(t, hasFinding(res, "bar.go"), "below the panel floor nothing is filtered at level "+level)
		eq(t, res.Summary.ConsensusFiltered, 0, "the panel floor is level-independent at level "+level)
	}
}

// TestConsensusLevel_ExemptionsUnchangedAtEveryLevel (AC5a): consensusExempt is
// identical at every level — a security-category singleton survives under
// lenient and strict alike, not because of the level but because it is exempt.
func TestConsensusLevel_ExemptionsUnchangedAtEveryLevel(t *testing.T) {
	// The security singleton is demoted to ConfLow by a low-trust prior, so under
	// BOTH strict and lenient it is a droppable singleton on the bar alone — only
	// consensusExempt keeps it. That makes this a real exemption test rather than
	// a restatement of the bar.
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("LOW", "sec.go", 10, "user input reaches the query unescaped", "security", "a"),
		}},
		{Name: "b", Findings: []Finding{
			cf("LOW", "bar.go", 20, "unused import lingers in this file", "style", "b"),
		}},
		{Name: "c", Findings: []Finding{
			cf("LOW", "baz.go", 30, "this name reads ambiguously here", "style", "c"),
		}},
	}
	priors := map[string]float64{"a": 0.1, "b": 0.1, "c": 0.1} // all demoted to ConfLow

	for _, level := range []string{ConsensusStrict, ConsensusLenient} {
		res := Reconcile(sources, Options{Consensus: level, TrustPriors: priors})
		isTrue(t, hasFinding(res, "sec.go"),
			"a security-category ConfLow singleton is exempt at level "+level)
		isTrue(t, !hasFinding(res, "bar.go"),
			"a non-exempt ConfLow singleton is still filtered at level "+level)
	}
}

// TestConsensusLevel_TrustExemptSurvivesStrict (AC5b) is the regression guard the
// epic names explicitly: restructuring the filter block must not drop the
// !trustExempt(...) term. Under strict, a high-trust reviewer's singleton is
// kept ONLY by trustExempt — nothing else in the predicate spares it.
func TestConsensusLevel_TrustExemptSurvivesStrict(t *testing.T) {
	priors := map[string]float64{"a": 1.0} // at/above trustHighThreshold

	res := Reconcile(levelPanel(), Options{Consensus: ConsensusStrict, TrustPriors: priors})
	isTrue(t, hasFinding(res, "foo.go"), "a high-trust singleton survives strict via trustExempt")
	isTrue(t, !hasFinding(res, "bar.go"), "an untrusted singleton is still filtered under strict")

	// Scope note — strict is the ONLY level at which this term is observable, so
	// this test deliberately asserts nothing at lenient. Sparing a singleton via
	// trustExempt under lenient would require a ConfLow finding whose sole
	// reviewer is high-trust, and that pairing is unreachable at reconcile time
	// by construction: demoteByTrust is the only path to ConfLow here and it
	// fires only at/below trustLowThreshold (0.3), while trustExempt requires
	// at/above trustHighThreshold (0.7). Every reachable lenient singleton is
	// ConfMedium, which the lenient bar keeps on its own.
	//
	// The term is therefore retained defensively rather than covered: a mutation
	// narrowing the predicate to `floor == ConfHigh && trustExempt(...)` is
	// behaviour-preserving today and no test can catch it. The strict assertions
	// above are the real AC5b guard — they fail the moment the term is dropped
	// outright. Do not add a direct trustExempt(...) unit call back here: it
	// reads as lenient coverage while exercising none of the filter block.
}

// TestConsensusSingletonAt_Bar pins the parameterized predicate directly against
// the confidence ladder, independent of any pipeline wiring.
func TestConsensusSingletonAt_Bar(t *testing.T) {
	cases := []struct {
		confidence, floor string
		wantSingleton     bool
	}{
		{ConfMedium, ConfHigh, true},    // strict: MEDIUM is droppable
		{ConfLow, ConfHigh, true},       // strict: LOW is droppable
		{ConfHigh, ConfHigh, false},     // strict: HIGH is corroborated
		{ConfMedium, ConfMedium, false}, /* lenient: MEDIUM is kept */
		{ConfLow, ConfMedium, true},     // lenient: LOW is still droppable
		{ConfHigh, ConfMedium, false},   // lenient: HIGH is kept
		{ConfidenceVerified, ConfHigh, false},
		{ConfidenceVerified, ConfMedium, false},
	}
	for _, c := range cases {
		got := consensusSingletonAt(Merged{Finding{Confidence: c.confidence}}, c.floor)
		eq(t, got, c.wantSingleton, c.confidence+" at floor "+c.floor)
	}
}

// TestConsensusSingletonAt_UnrecognizedFloorKeeps guards the fail-safe posture:
// a floor that names no recognized confidence tier (e.g. the "" consensusFloor
// historically returned for off) must NOT make every finding a drop candidate.
// ConfidenceAtOrAbove fails closed on such a floor and consensusSingletonAt
// negates it, so without a guard the whole panel — including VERIFIED findings —
// would be droppable if a caller ever skipped the enabled check.
func TestConsensusSingletonAt_UnrecognizedFloorKeeps(t *testing.T) {
	for _, floor := range []string{"", "bogus"} {
		for _, conf := range []string{ConfLow, ConfMedium, ConfHigh, ConfidenceVerified} {
			isTrue(t, !consensusSingletonAt(Merged{Finding{Confidence: conf}}, floor),
				conf+" at unrecognized floor must not be a drop candidate")
		}
	}
}

// TestConsensusFloor_OffReturnsUsableFloor pins the off case returning a real
// floor alongside enabled=false, so a dropped enabled check degrades to strict
// rather than to delete-everything.
func TestConsensusFloor_OffReturnsUsableFloor(t *testing.T) {
	floor, enabled := consensusFloor(ConsensusOff)
	isTrue(t, !enabled, "off disables the filter")
	eq(t, floor, ConfHigh, "off still returns a usable (strict) floor")
}

// TestNormalizeConsensus_Vocabulary pins the closed enum and its case- and
// whitespace-insensitivity, the contract both the CLI and MCP surfaces validate
// against.
func TestNormalizeConsensus_Vocabulary(t *testing.T) {
	valid := map[string]string{
		"":           ConsensusStrict, // unset means strict
		"strict":     ConsensusStrict,
		"STRICT":     ConsensusStrict,
		"  Strict  ": ConsensusStrict,
		"lenient":    ConsensusLenient,
		"LeNiEnT":    ConsensusLenient,
		"off":        ConsensusOff,
		"  OFF\t":    ConsensusOff,
	}
	for in, want := range valid {
		got, ok := NormalizeConsensus(in)
		isTrue(t, ok, "expected "+in+" to be valid")
		eq(t, got, want, "canonicalization of "+in)
	}

	for _, in := range []string{"bogus", "strictly", "on", "none", "0"} {
		_, ok := NormalizeConsensus(in)
		isTrue(t, !ok, "expected "+in+" to be rejected")
	}

	// ConsensusLevels is the single list every surface names in its error text.
	deepEq(t, ConsensusLevels, []string{ConsensusStrict, ConsensusLenient, ConsensusOff},
		"documentation order: default first")
}

// TestConsensusFloor_LevelsDistinct pins each level in the public vocabulary to
// a distinct (floor, enabled) pair, so a future fourth level cannot be added to
// the const block and ConsensusLevels and then silently behave as strict —
// without this guard nothing fails when the switch is not extended. (Verified
// by mutation: appending a dummy constant to ConsensusLevels makes the pair
// collide with strict's and this test fails.)
func TestConsensusFloor_LevelsDistinct(t *testing.T) {
	type pair struct {
		floor   string
		enabled bool
	}
	seen := map[pair]string{}
	for _, level := range ConsensusLevels {
		floor, enabled := consensusFloor(level)
		p := pair{floor, enabled}
		if prev, dup := seen[p]; dup {
			t.Errorf("levels %q and %q map to the same (floor, enabled) pair %v", prev, level, p)
		}
		seen[p] = level
	}
}

// TestDemoteByTrust_ObservableViaConsensusLevel closes epic 35.9's AC2, the half
// that was unverifiable until the levels existed.
//
// It mirrors TestDemoteByTrust_LowTrustSingletonDemotedToConfLow (trust_test.go)
// but drops that test's security-category escape hatch. That test had to label
// its finding "security" so consensusExempt would spare it — otherwise strict
// sidecarred the demoted finding and the demotion could not be observed at all.
// Here the finding is a plain style singleton with no exemption of any kind, so
// the LEVEL is the only thing keeping it alive:
//
//   - strict  — sidecarred (the pre-35.9.1 blind spot this test documents)
//   - lenient — still sidecarred: ConfLow is below the MEDIUM bar
//   - off     — kept, carrying Confidence == LOW, which is the end-to-end proof
//     that demoteByTrust actually fired
func TestDemoteByTrust_ObservableViaConsensusLevel(t *testing.T) {
	sources := func() []Source {
		return []Source{
			{Name: "a", Findings: []Finding{
				cf("MEDIUM", "foo.go", 10, "unused import lingers in this file", "style", "flaky"),
			}},
			{Name: "b", Findings: []Finding{
				cf("MEDIUM", "bar.go", 20, "this name reads ambiguously here", "style", "stranger"),
			}},
			{Name: "c", Findings: []Finding{
				cf("MEDIUM", "baz.go", 30, "request body is not validated", "correctness", "third"),
			}},
		}
	}
	priors := map[string]float64{"flaky": trustLowThreshold}

	// findConfidence returns the confidence of the finding at file, and whether it
	// survived to Findings at all.
	findConfidence := func(res Result, file string) (string, bool) {
		for _, m := range res.Findings {
			if m.File == file {
				return m.Confidence, true
			}
		}
		return "", false
	}

	// strict: the demoted singleton is sidecarred, so the demotion is invisible in
	// findings.json. This is the baseline that made 35.9's AC2 unverifiable.
	strict := Reconcile(sources(), Options{Consensus: ConsensusStrict, TrustPriors: priors})
	_, survivedStrict := findConfidence(strict, "foo.go")
	isTrue(t, !survivedStrict, "strict sidecars the demoted non-exempt singleton")
	isTrue(t, inAmbiguousSingleton(strict, "foo.go"), "and it is recoverable from the sidecar")

	// lenient: ConfLow is still below the MEDIUM bar, so it is sidecarred here too
	// — while the two undemoted ConfMedium singletons survive. That contrast is
	// what proves the demotion changed this finding's fate specifically.
	lenient := Reconcile(sources(), Options{Consensus: ConsensusLenient, TrustPriors: priors})
	_, survivedLenient := findConfidence(lenient, "foo.go")
	isTrue(t, !survivedLenient, "lenient sidecars the demoted ConfLow singleton")
	isTrue(t, inAmbiguousSingleton(lenient, "foo.go"), "and it is recoverable from the sidecar")
	isTrue(t, hasFinding(lenient, "bar.go"), "an undemoted ConfMedium singleton survives lenient")
	isTrue(t, hasFinding(lenient, "baz.go"), "an undemoted ConfMedium singleton survives lenient")
	eq(t, lenient.Summary.ConsensusFiltered, 1, "exactly the demoted finding is filtered under lenient")

	// off: the filter is inert, so the demoted finding reaches findings.json still
	// carrying ConfLow — the end-to-end observation epic 35.9 AC2 asked for.
	off := Reconcile(sources(), Options{Consensus: ConsensusOff, TrustPriors: priors})
	conf, survivedOff := findConfidence(off, "foo.go")
	isTrue(t, survivedOff, "off keeps the demoted singleton in findings.json")
	eq(t, conf, ConfLow, "and it carries Confidence == LOW, proving demoteByTrust fired")
	eq(t, off.Summary.ConsensusFiltered, 0, "off leaves ConsensusFiltered at 0")

	// The demotion is level-independent: without the trust prior the same finding
	// is ConfMedium under off, so ConfLow above is attributable to the prior and
	// not to anything the level did.
	noPriors := Reconcile(sources(), Options{Consensus: ConsensusOff})
	conf, survived := findConfidence(noPriors, "foo.go")
	isTrue(t, survived, "off keeps the singleton with no priors too")
	eq(t, conf, ConfMedium, "without the low-trust prior the same finding is ConfMedium")
}
