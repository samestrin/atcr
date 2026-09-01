package cli

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/internal/stream"
)

// validateSuitePublishableCaseIDs rejects a suite whose case ids — or whose published
// suite identity, m.Suite and m.SuiteVersion — cannot survive publication intact,
// BEFORE any reviewer is invoked.
//
// buildRunResult copies m.Cases[i].ID into SuiteCaseIDs verbatim, and submission
// schema 2 PUBLISHES that array — so validateScrubbedCaseIDs (cli/benchmark_coverage.go)
// hard-rejects any id the publication scrub rewrites. That gate is a consumer-side
// check that runs at `benchmark export`, i.e. after the whole panel has already been
// paid for: a legitimate importer-built suite is then permanently unexportable and
// the operator only learns so hours later. This applies the identical rule at load
// time, where the remedy costs nothing. The suite identity is gated at load for the
// same reason: BuildSubmission publishes m.Suite and m.SuiteVersion scrubbed by the
// same pass, and validateSuiteIdentityForPublication (cli/benchmark_coverage.go)
// hard-rejects the identity arms at export. The name survives from when the function
// covered case ids only; the identity arm's details are documented in the body.
//
// It is the case-id counterpart of the post-scrub reviewer-IDENTITY collision check
// in buildRunResult, and lives here rather than there for the same reason that check
// lives at its own producer point: buildRunResult runs at the END of the fold, so a
// rejection there would still follow the full run.
//
// Only the printability and rewrite rules are needed. Two DISTINCT raw ids reaching
// one published id means at least one was rewritten, so the collision shape has no
// reachable diagnostic of its own — the reasoning documented on
// validateScrubbedCaseIDs. Verbatim-duplicate raw ids belong to manifest validation.
//
// Errors name the PRE-scrub value and the SUITE MANIFEST: the scrubbed value is empty
// or rewritten by construction, and suite_case_ids and the envelope identity are
// verbatim copies of the manifest, so editing a run-result would be the wrong action.
func validateSuitePublishableCaseIDs(m *benchmark.Manifest, suitePath string) error {
	// The suite IDENTITY is published scrubbed by the same BuildSubmission pass that
	// publishes the ids, and validateSuiteIdentityForPublication hard-rejects all three
	// arms at export. Checking only m.Cases here left that half enforced at export and
	// nowhere else: `benchmark verify` printed "valid" for a manifest export rejects,
	// and `benchmark run` paid for the whole reviewer panel before the rejection
	// surfaced. Identity is checked BEFORE the cases: it names the whole document, so
	// it is the defect to report first when both are present.
	//
	// consequence and remedy are carried PER FIELD rather than shared across the two.
	// One constant pair reported a bad suite_version with "rename the suite in the
	// suite manifest", and acting on that misdirection is not a harmless no-op:
	// ReproHashManifest length-prefixes m.Suite into the hash and validateCheckpoint
	// compares ReproHash, Suite AND SuiteVersion, so renaming the suite makes the next
	// `benchmark run --checkpoint` fail with errCheckpointSuiteMismatch — discarding
	// the paid work of every completed case — and then re-raises the identical
	// suite_version error.
	//
	// Both strings are written out per field rather than interpolated from noun. The
	// suite arm is pre-existing behavior and stays BYTE-IDENTICAL, which a
	// "rename the "+noun form would silently break ("rename the suite name in the
	// suite manifest"), and the verbs differ anyway: a suite is renamed, a version is
	// changed.
	// published names the PUBLISHED field for the empty arm, which must tell the
	// operator which field goes empty — the manifest-side noun does not: a case id
	// publishes inside suite_case_ids rather than as "a case", and the identity
	// publishes in the envelope. Like consequence and remedy it is written out per
	// field rather than interpolated from noun.
	for _, f := range []struct{ noun, published, value, consequence, remedy string }{
		{"suite name", "the envelope's suite name", m.Suite,
			"the published envelope must name the same suite the manifest does",
			"rename the suite in the suite manifest"},
		{"suite_version", "the envelope's suite_version", m.SuiteVersion,
			"the published envelope must name the same suite_version the manifest does",
			"change suite_version in the suite manifest"},
	} {
		if err := checkPublishable(suitePath, "declares "+f.noun, f.value, f.published, f.consequence, f.remedy); err != nil {
			return err
		}
	}
	for _, c := range m.Cases {
		if err := checkPublishable(suitePath, "declares case", c.ID, "suite_case_ids", "the published suite_case_ids must name the same cases as the manifest", "rename the case in the suite manifest"); err != nil {
			return err
		}
	}
	return nil
}

// checkPublishable is the three-arm publication predicate the case ids and the suite
// identity now share: a value must carry no control or format rune, must not scrub
// away, and must not be rewritten by the scrub.
//
// One helper rather than two copies is what makes "verify and run agree on what a
// publishable suite is" a property of the code rather than of two edits staying in
// sync. EXPORT IS NOT ON THIS PATH: validateSuiteIdentityForPublication
// (cli/benchmark_coverage.go) applies its own independent three-arm copy to the
// run-result values at export, so the load-time gate and the export gate remain two
// copies that must be kept in step — this helper deduplicates only the load side.
// Arm ORDER matters and is fixed here: printability first (the defect a
// reader cannot see), then empty (the sharper diagnostic for a value that scrubs away,
// naming the published field that goes empty), then rewrite.
//
// Every message names the PRE-scrub value under %q - the published value is empty or
// rewritten by construction, so it identifies no line to edit - and ends with remedy,
// which names the MANIFEST: suite_case_ids and the envelope identity are verbatim
// copies of it, so editing a run-result would be the wrong action.
func checkPublishable(suitePath, subject, value, published, consequence, remedy string) error {
	if r, bad := firstNonPrintingRune(value); bad {
		return fmt.Errorf("suite %s %s %q, which contains a non-printing rune (U+%04X); "+
			"control and format runes are invisible or reorder text in the published document, "+
			"so %s",
			suitePath, subject, value, r, remedy)
	}
	s := scorecard.ScrubPublicString(value)
	if s == "" {
		return fmt.Errorf("suite %s %s %q, which is empty once scrubbed for publication; "+
			"the run would publish \"\" in %s and be rejected at export, "+
			"so %s",
			suitePath, subject, value, published, remedy)
	}
	if s != value {
		return fmt.Errorf("suite %s %s %q, which the publication scrub rewrites to %q; "+
			"%s, "+
			"so %s",
			suitePath, subject, value, s, consequence, remedy)
	}
	return nil
}

// validatePublishableReviewerRoster rejects a run whose CONFIGURED reviewer panel
// carries an identity the public envelope cannot publish intact.
//
// It is the producer-side counterpart of the reviewer-identity gate
// validateRunResultForPublication applies at export (cli/benchmark.go). That gate is
// correct but sits at the consumer: nothing stopped `benchmark run` from PRODUCING the
// artifact it rejects. The fold's scrub is scorecard.ScrubPublicRecord, which provably
// leaves control (Cc) and format (Cf) runes alone, and the post-scrub collision check
// in buildRunResult cannot see one either — two identities differing only by an
// invisible rune do not collide after a scrub that keeps it. So a soft hyphen (U+00AD)
// or zero-width space (U+200B) pasted into a registry `model:` flowed all the way into
// a finished run-result that export then refused permanently, after the whole panel had
// been paid for, with the remedy in registry.yaml rather than in the file the operator
// was handed.
//
// It runs at LOAD, beside validateSuitePublishableCaseIDs, for the identical reason
// that one does: a rejection raised at the end of the fold still follows the full run.
//
// Only the printability arm belongs here. The empty-once-scrubbed and scrub-rewrites
// arms are checked on the REALIZED identity downstream (buildRunResult's collision
// guard and the export gate's TrimSpace arm), because the configured model is not the
// published one — reviewerModel prefers the usage-reported and fallback models over the
// registry — so rejecting a configured value the run would never publish would fail a
// panel for a row it does not emit.
//
// The gate must cover every identity the run can actually publish, or the
// before-payment guarantee is only apparent. Three sources feed it, and all three are
// known at load:
//
//   - BOTH lanes. cfg.Project.SerialAgents is a second roster fanout builds slots for
//     exactly as it does the parallel one (fanout.rosterNames is the union). Iterating
//     Agents alone left every serial reviewer covered only by the fold backstop —
//     which is to say, covered only after the panel was paid for.
//   - The FALLBACK chain. reviewerModel prefers a.FallbackModel over the primary, and
//     every fallback is another registry entry reachable by name, so a rune in a
//     `-backup` agent's model is knowable here rather than hours later.
//   - The REALIZED persona. reviewerPersona falls back to the AGENT NAME when the
//     registry persona is empty, and a project roster entry is validated only for
//     non-emptiness — so the agent key itself is a publishable identity.
//
// Errors name the AGENT and the REGISTRY: the identity is a verbatim copy of the
// registry entry, so editing a produced run-result would be the wrong action.
func validatePublishableReviewerRoster(cfg *fanout.ReviewConfig) error {
	// No nil guard on cfg/Project/Registry, matching rosterSignature below: both are
	// reached only from executeBenchmarkRun, which is handed a fully built config by
	// its caller. A guard here would be unreachable by construction, so it could only
	// be covered by a test asserting a state the CLI cannot produce.
	//
	// Sorted, so a panel with two offending lanes reports the same one on every run
	// rather than whichever the map iteration reached first.
	names := reviewerRoster(cfg)
	sort.Strings(names)
	// check reports one offending identity. roster is the configured entry the operator
	// reads; cur is the entry the defect is actually in — a fallback link is named by
	// BOTH, or the operator cannot find the row they configured.
	check := func(roster, cur, field, value string) error {
		r, bad := firstNonPrintingRune(value)
		if !bad {
			return nil
		}
		via := ""
		if cur != roster {
			via = fmt.Sprintf(" (reached as a fallback of %q)", roster)
		}
		return fmt.Errorf("reviewer agent %q%s declares %s %q, which contains a non-printing rune (U+%04X); "+
			"control and format runes are invisible or reorder text in the published document, "+
			"so a leaderboard row can be misattributed to a model that was never measured — "+
			"rename the %s in the reviewer registry",
			cur, via, field, value, r, field)
	}

	for _, n := range names {
		// The PERSONA arm covers the ROSTER agent only, never the chain. reviewerPersona
		// resolves cfg.Registry.Agents[a.Agent].Persona where a.Agent is always the
		// PRIMARY slot name even when a fallback served the case — the contract
		// reviewerKey states — so a fallback entry's own persona is never published, and
		// its empty-persona substitute is the ROSTER name, never the chain entry's.
		// Walking the chain here refused whole panels for strings that print nowhere.
		persona := cfg.Registry.Agents[n].Persona
		if persona == "" {
			persona = n
		}
		if err := check(n, n, "persona", persona); err != nil {
			return err
		}

		// The MODEL arm DOES walk the full chain: reviewerModel prefers a.FallbackModel,
		// so a fallback's model is genuinely published when it serves a case.
		//
		// seen bounds the walk. registry.Validate already rejects a cycle, but this runs
		// on a config that reached us however it reached us, and an infinite loop inside
		// a pre-flight would be a worse failure than the one it guards.
		seen := map[string]bool{}
		for cur := n; cur != "" && !seen[cur]; cur = cfg.Registry.Agents[cur].Fallback {
			seen[cur] = true
			if err := check(n, cur, "model", cfg.Registry.Agents[cur].Model); err != nil {
				return err
			}
		}
	}
	return nil
}

// executeBenchmarkRun executes a benchmark suite end to end and returns the
// suite-tagged benchmark.RunResult that `atcr benchmark export` consumes. It loads
// + validates the suite (benchmark.Load), then for each case ingests the case diff
// through the EXACT production review path (fanout.PrepareReviewFromDiff →
// fanout.ExecuteReview, the diff-file ingestion entry Epic 10.1 added), reads the
// per-reviewer findings + usage from the review's pool artifacts, scores the
// findings against the case's expected categories (benchmark.Score), and aggregates
// one scorecard.PublicRecord per reviewer.
//
// It lives in cmd/atcr — the composition root — rather than internal/benchmark so
// that package stays the light suite-contract + scorer leaf (no live-LLM
// dependency); the orchestration is the layer that wires the contract to the
// fan-out engine.
//
// The Completer is injected so the CLI passes the real llmclient and tests pass a
// stub (no network). generatedAt is injected (not time.Now) so two runs over the
// same suite + transcript produce a byte-identical RunResult — the reproducibility
// contract export relies on. Each case's review artifacts are written under a temp
// directory that is removed before the function returns; only the scored findings
// flow into the result, so the temp path never affects output.
func executeBenchmarkRun(ctx context.Context, cfg *fanout.ReviewConfig, completer fanout.Completer, suitePath string, generatedAt time.Time, checkpointPath string) (*benchmark.RunResult, error) {
	m, err := benchmark.Load(suitePath)
	if err != nil {
		return nil, err
	}
	// Pre-flight, before a single reviewer runs: an id that cannot publish makes the
	// finished run unexportable, and the export gate would only say so afterwards.
	if err := validateSuitePublishableCaseIDs(m, suitePath); err != nil {
		return nil, err
	}
	// The reviewer half of the same pre-flight, and for the same reason: an identity
	// that cannot publish makes the finished run unexportable, and both the export gate
	// and buildRunResult's backstop would only say so after the panel was paid for.
	if err := validatePublishableReviewerRoster(cfg); err != nil {
		return nil, err
	}

	// Opt-in checkpointing (Epic 10.3): when checkpointPath is set, each scored case
	// is persisted before the next begins so a transient failure does not forfeit the
	// paid work of the cases that already completed. An empty path keeps the 10.2
	// behavior verbatim (no read, no write).
	var cp *runCheckpoint
	var done map[int]checkpointCase
	if checkpointPath != "" {
		curHash, herr := benchmark.ReproHashManifest(m, suitePath)
		if herr != nil {
			return nil, fmt.Errorf("hashing suite for checkpoint: %w", herr)
		}
		existing, lerr := loadCheckpoint(checkpointPath)
		if lerr != nil {
			return nil, lerr
		}
		roster := rosterSignature(cfg)
		legacyRoster := rosterSignatureOf(cfg, cfg.Project.Agents)
		if existing != nil {
			// Suite-identity guard (AC4): a checkpoint from a different or changed
			// suite is rejected, never silently mixed into this run. The roster guard
			// catches a changed reviewer panel — orthogonal to ReproHash, which hashes
			// only suite content.
			if verr := validateCheckpoint(existing, curHash, m.Suite, m.SuiteVersion); verr != nil {
				return nil, verr
			}
			if verr := validateCheckpointRoster(existing, roster, legacyRoster); verr != nil {
				return nil, verr
			}
			cp = existing
		} else {
			cp = &runCheckpoint{ReproHash: curHash, Suite: m.Suite, SuiteVersion: m.SuiteVersion, Roster: roster, RosterFormat: rosterFormatUnion}
		}
		done = cp.doneIndex()
		// validateCheckpointIntegrity runs at load, before the suite is in scope, so
		// it can only reject negative indices. The upper bound is checked here, where
		// len(m.Cases) is known: an entry indexed beyond the suite was recorded
		// against a different case list — left unchecked it would be counted in the
		// replayed total yet never replayed by the loop below, silently vanishing
		// from the score and rendering a negative remaining count.
		for idx := range done {
			if idx < 0 || idx >= len(m.Cases) {
				return nil, fmt.Errorf("%w: case index %d outside [0, %d)", errCheckpointCorrupt, idx, len(m.Cases))
			}
		}
		if existing != nil {
			replayed := len(done)
			remaining := len(m.Cases) - replayed
			fmt.Fprintf(os.Stderr, "Resuming benchmark: replayed %d case(s), %d remaining to execute\n", replayed, remaining)
		}
	}

	tmp, err := os.MkdirTemp("", "atcr-benchmark-")
	if err != nil {
		return nil, fmt.Errorf("creating benchmark work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	accs := map[reviewerKey]*reviewerAcc{}
	var order []reviewerKey // realized identities in first-sighting order; buildRunResult sorts them for deterministic aggregation

	for i, c := range m.Cases {
		// Resume: a case already in the checkpoint is replayed into the accumulator
		// without re-executing (and re-paying for) it (AC2). Replaying in case-index
		// order preserves the deterministic aggregation the reproducibility contract
		// depends on (AC3).
		if entry, ok := done[i]; ok {
			// ReproHash is order-independent (it sorts cases by id), so a reordered
			// suite shares the hash but remaps indices. Guard the per-index identity
			// too: a checkpoint entry whose recorded id no longer matches the suite's
			// case at this index means the suite changed — fail closed rather than
			// replay a score against the wrong case.
			if entry.CaseID != c.ID {
				return nil, fmt.Errorf("%w: checkpoint case at index %d is %q but the suite has %q there; remove the checkpoint to start fresh",
					errCheckpointCaseMismatch, i, entry.CaseID, c.ID)
			}
			if err := replayCheckpointCase(accs, &order, entry, c.ExpectedCategories); err != nil {
				return nil, err
			}
			continue
		}

		diff, err := os.ReadFile(filepath.Join(suitePath, c.Diff))
		if err != nil {
			return nil, fmt.Errorf("reading case %q diff: %w", c.ID, err)
		}

		// Range-less request writing to an isolated per-case dir: no git range, no
		// .atcr/latest repoint (OutputDir suppresses it). The dir is keyed by case
		// INDEX, not id, so two distinct ids that share a path basename (a case id
		// may legally contain '/') can never collide and overwrite each other's
		// review. The date/suffix only feed the review id, never the RunResult, so
		// fixed values keep the run hermetic.
		req := fanout.ReviewRequest{
			Root:       tmp,
			OutputDir:  filepath.Join(tmp, fmt.Sprintf("case-%d", i)),
			Branch:     "benchmark",
			Date:       "2026-01-01",
			TimeSuffix: "000000",
			StartedAt:  time.Unix(0, 0).UTC(),
		}
		prep, err := fanout.PrepareReviewFromDiff(ctx, cfg, req, string(diff))
		if err != nil {
			return nil, fmt.Errorf("preparing case %q: %w", c.ID, err)
		}
		// The reviewer count is the FULL roster, both lanes: fanout builds slots for
		// SerialAgents exactly as it does for Agents, so counting the parallel lane
		// alone under-reports every serial reviewer on every case.
		log.FromContext(ctx).Info("benchmark case executing", "case", c.ID, "reviewers", len(reviewerRoster(cfg)))
		res, err := fanout.ExecuteReview(ctx, completer, prep)
		if err != nil {
			return nil, fmt.Errorf("executing case %q: %w", c.ID, err)
		}

		summary, err := fanout.ReadPoolSummary(res.Dir)
		if err != nil {
			return nil, fmt.Errorf("reading pool summary for case %q: %w", c.ID, err)
		}
		raisedByReviewer, err := readCaseFindings(res.Dir)
		if err != nil {
			return nil, fmt.Errorf("reading findings for case %q: %w", c.ID, err)
		}

		// Iterate the full agent roster (including failed agents, which raised
		// nothing) so every reviewer is scored on every case — a missed case is
		// recall 0, not an absent record.
		var caseReviewers []checkpointReviewer
		for _, a := range summary.Agents {
			model := reviewerModel(cfg, a)
			persona := reviewerPersona(cfg, a.Agent)
			raised := raisedByReviewer[a.Agent]
			// Cost + latency are usage-gated: a completer that reports no token
			// usage (the test stub) contributes neither, keeping the score
			// deterministic. status.json only records Model/tokens when usage > 0.
			usageReported := a.TokensIn > 0 || a.TokensOut > 0
			var cost float64
			var latency int64
			if usageReported {
				cost = llmclient.ComputeCostUSD(a.Model, a.TokensIn, a.TokensOut)
				latency = a.DurationMS
			}

			outcome := reviewerOutcome(a, raised)

			if err := applyReviewerOutcome(accs, &order, reviewerCaseOutcome{
				model:         model,
				persona:       persona,
				caseID:        c.ID,
				expected:      c.ExpectedCategories,
				raised:        raised,
				usageReported: usageReported,
				costUSD:       cost,
				latencyMS:     latency,
				outcome:       outcome,
				fallbackUsed:  a.FallbackUsed,
				agent:         a.Agent,
			}); err != nil {
				return nil, fmt.Errorf("scoring case %q: %w", c.ID, err)
			}

			if cp != nil {
				caseReviewers = append(caseReviewers, checkpointReviewer{
					Agent:         a.Agent,
					Model:         model,
					Persona:       persona,
					Raised:        raised,
					UsageReported: usageReported,
					CostUSD:       cost,
					LatencyMS:     latency,
					Outcome:       outcome,
					FallbackUsed:  a.FallbackUsed,
				})
			}
		}

		// Checkpoint the scored case before the loop advances to case i+1 (AC1): the
		// atomic write means a process killed mid-suite leaves a checkpoint holding
		// exactly the cases that completed.
		if cp != nil {
			cp.Cases = append(cp.Cases, checkpointCase{Index: i, CaseID: c.ID, Expected: c.ExpectedCategories, Reviewers: caseReviewers})
			if werr := saveCheckpoint(checkpointPath, cp); werr != nil {
				return nil, fmt.Errorf("writing checkpoint for case %q: %w", c.ID, werr)
			}
		}
	}

	return buildRunResult(accs, order, m, generatedAt)
}

// buildRunResult folds the finished accumulator into the suite-tagged RunResult:
// the scored reviewer rows, the run diagnostic, and the coverage sibling that names
// the cases behind each row.
//
// It is separated from executeBenchmarkRun so the aggregation can be driven directly
// from a recorded checkpoint — the Run B fixture folds through this exact path in
// tests, without a suite on disk, a Completer, or a network.
//
// It fails when two DISTINCT raw identities scrub to the same public one:
// scorecard.scrubField is not injective (it deletes path-, home- and
// credential-shaped tokens), and emitting two coverage rows of identical public
// identity would be rejected by the export gate as hand-assembled — misdiagnosing a
// legitimate run as tampering. The collision is the producer's to report, so the
// error names both pre-scrub identities.
func buildRunResult(accs map[reviewerKey]*reviewerAcc, order []reviewerKey, m *benchmark.Manifest, generatedAt time.Time) (*benchmark.RunResult, error) {
	// Scrub the identity BEFORE sorting and before it is written to either slice.
	//
	// benchmark.Score re-scrubs the rows it emits (scorecard.ScrubPublicRecord), so
	// without this the reviewer rows would carry post-scrub identities while the
	// coverage rows carried pre-scrub ones. Any identity the scrub rewrites — it
	// strips path-, email- and credential-shaped substrings — would then fail to join
	// at export, turning a legitimate full-coverage run into a hard
	// "no coverage recorded" rejection. Scrubbing here makes both sides identical by
	// construction, and Score's own pass is then idempotent.
	//
	// That last clause is a real guarantee, not an assumption: scorecard.scrubField
	// iterates to a fixed point and TestScrubField_IsIdempotent pins it. It did not
	// always hold — one pass could expose a match for an earlier rule, so
	// "bedrock@us-east-1/claude" scrubbed once to "/claude" and twice to "" — and the
	// three arrays below then carried two different identities for one reviewer,
	// because coverage is written from `id` here while reviewers[] and
	// reviewer_vocabulary[] pass through a second scrub downstream.
	rows := make([]scoredRow, 0, len(order))
	scrubbed := make(map[reviewerKey]reviewerKey, len(order)) // public identity -> pre-scrub key
	for _, k := range order {
		// The REALIZED half of the printability rule validatePublishableReviewerRoster
		// applies to the configured panel. Only half of a reviewer identity is
		// configured: reviewerModel prefers the usage-reported model and then the
		// fallback model over the registry entry, so a Cc/Cf rune arriving in a
		// provider's own usage payload never passes through the roster gate. Without
		// this arm the fold would still emit an artifact export rejects permanently.
		//
		// Checked BEFORE the scrub, like every other printability arm: ScrubPublicString
		// provably leaves Cc and Cf alone, so the rune survives into the published
		// envelope and neither the collision guard below nor the export gate's
		// empty-once-scrubbed arm can see it — the value is non-empty on both sides and
		// two identities differing only by an invisible rune do not collide.
		for _, f := range []struct{ name, value string }{
			{"model", k.model},
			{"persona", k.persona},
		} {
			if r, bad := firstNonPrintingRune(f.value); bad {
				// The remedy is named because this arm, unlike the load-time gate, can
				// fire on an identity NO local file contains: a model id echoed back in
				// a provider's usage payload. Failing here forfeits the run rather than
				// writing an artifact export would refuse permanently, so the message
				// has to say where to look and what to do with the checkpoint.
				//
				// It names REPAIR, not discard. The offending value is the per-case
				// `model` field the checkpoint recorded, and a resume validates only the
				// suite identity plus rosterSignature — which reads the registry, not the
				// checkpoint — so correcting that string resumes for free. Sending the
				// operator to discard re-pays the whole paid suite for no reason.
				return nil, fmt.Errorf("reviewer identity %s %q contains a non-printing rune (U+%04X); "+
					"control and format runes are invisible or reorder text in the published document, "+
					"so a leaderboard row can be misattributed to a model that was never measured — "+
					"if the reviewer registry is clean the id came from the provider's own usage report, "+
					"so pin or repoint that model; a checkpoint holding this identity replays into the "+
					"same rejection until the recorded model is corrected in the checkpoint file "+
					"(discarding it re-runs the whole suite)",
					f.name, f.value, r)
			}
		}
		s := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: k.model, Persona: k.persona})
		id := reviewerKey{model: s.Model, persona: s.Persona}
		if prev, dup := scrubbed[id]; dup {
			return nil, fmt.Errorf("distinct reviewer identities %q/%q and %q/%q scrub to the same public identity %q/%q: "+
				"scorecard's path/credential scrub is not injective, so publishing would emit two coverage rows under one identity",
				prev.model, prev.persona, k.model, k.persona, id.model, id.persona)
		}
		scrubbed[id] = k
		rows = append(rows, scoredRow{id: id, acc: accs[k]})
	}
	// Sort the SCRUBBED identities so this order matches the order Score will sort
	// its (equally scrubbed) rows into — the property that keeps the two slices
	// positionally aligned.
	sortScoredRows(rows)

	reviewers := make([]benchmark.ReviewerScore, 0, len(rows))
	coverage := make([]benchmark.ReviewerCoverage, 0, len(rows))
	for _, r := range rows {
		acc, id := r.acc, r.id
		reviewers = append(reviewers, benchmark.ReviewerScore{
			Model:        id.model,
			Persona:      id.persona,
			Cases:        acc.cases,
			CostUSD:      acc.costUSD,
			LatencyP50MS: medianInt64(acc.latencies),
		})
		// Emitted in the same order as the reviewer rows, so a consumer can join
		// coverage to its row positionally as well as by identity. CaseIDs and
		// Outcomes are COPIED, not aliased: this function is the shared fold path
		// for fresh runs and recorded checkpoints, so the returned artifact must
		// not change if a caller keeps folding into accs afterwards.
		coverage = append(coverage, benchmark.ReviewerCoverage{
			Model:         id.model,
			Persona:       id.persona,
			CaseIDs:       append([]string(nil), acc.caseIDs...),
			Outcomes:      maps.Clone(acc.outcomes),
			FallbackCases: acc.fallbackCases,
		})
	}

	suiteCaseIDs := make([]string, len(m.Cases))
	for i, c := range m.Cases {
		suiteCaseIDs[i] = c.ID
	}

	return &benchmark.RunResult{
		Suite:        m.Suite,
		SuiteVersion: m.SuiteVersion,
		GeneratedAt:  generatedAt.UTC().Format(time.RFC3339),
		Reviewers:    benchmark.Score(reviewers),
		// Computed from the same accumulated ReviewerScores Score consumes, so a
		// resumed run reports the same rate as an uninterrupted one — replayed cases
		// fold in through applyReviewerOutcome before this point. nil when the run
		// raised no findings at all, which must not read as perfect agreement.
		OutOfVocabularyRate: benchmark.OutOfVocabularyRate(reviewers),
		// The same drift, attributed. Built from the SAME slice as the two values
		// above — which is what keeps the breakdown's totals equal to the scalar's,
		// and its rows positionally aligned with the Reviewers rows Score emits.
		Vocabulary:   benchmark.PerReviewerVocabulary(reviewers),
		SuiteCaseIDs: suiteCaseIDs,
		Coverage:     coverage,
	}, nil
}

// reviewerRoster returns the full configured reviewer panel: the parallel lane then
// the serial one, mirroring fanout.rosterNames, which is what actually builds the
// slots. Every caller that means "the reviewers this run has" reads it from here, so
// the load-time identity gate, the per-case log line, and the resume roster guard
// cannot disagree about who is on the panel. Iterating cfg.Project.Agents alone let
// rosterSignature compare EQUAL across a changed serial lane and resume, publishing
// two panels as one comparable measurement — the AC4 outcome the guard exists to stop.
//
// The result is a fresh slice: callers sort it in place.
func reviewerRoster(cfg *fanout.ReviewConfig) []string {
	names := make([]string, 0, len(cfg.Project.Agents)+len(cfg.Project.SerialAgents))
	names = append(names, cfg.Project.Agents...)
	names = append(names, cfg.Project.SerialAgents...)
	return names
}

// rosterSignature builds the deterministic "agent=model=persona" signature of the
// configured reviewer panel, sorted by agent name. It uses the CONFIGURED values
// (registry), not runtime usage-reported ones, so the same config always yields the
// same signature — the stable identity a resume compares against to reject a changed
// panel (AC4 roster guard). Persona is included because it is a behavioral modifier
// (system prompt) that can change reviewer outputs even when model stays the same.
// An agent with no configured model or persona contributes an empty component,
// which still distinguishes it from a later-configured one.
func rosterSignature(cfg *fanout.ReviewConfig) []string {
	return rosterSignatureOf(cfg, reviewerRoster(cfg))
}

// rosterSignatureOf builds the signature over an ARBITRARY subset of the panel, so a
// resume can compare against the shape a previous binary recorded as well as the one
// this binary writes. rosterSignature is it applied to the full roster; the resume
// guard also applies it to cfg.Project.Agents alone, which is exactly what the
// released binary wrote before this branch unioned the serial lane in.
//
// It sorts a COPY: cfg.Project.Agents is the live config, and sorting it in place
// would rewrite the declared lane order for every caller downstream.
func rosterSignatureOf(cfg *fanout.ReviewConfig, names []string) []string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	sig := make([]string, len(sorted))
	for i, n := range sorted {
		sig[i] = n + "=" + cfg.Registry.Agents[n].Model + "=" + cfg.Registry.Agents[n].Persona
	}
	return sig
}

// reviewerKey identifies one leaderboard row by (REALIZED model, CONFIGURED
// persona). Only the model half is realized: reviewerModel resolves the model that
// actually served the case. The persona half is always the slot's configured value —
// reviewerPersona resolves cfg.Registry.Agents[a.Agent].Persona where a.Agent is the
// PRIMARY slot name even when a fallback served the case. That is correct only by
// registry convention (every -backup agent declares its primary's persona); a backup
// declaring its own persona would publish the primary's regardless of which system
// prompt actually ran.
//
// It is deliberately the same identity benchmark.Score already sorts and publishes
// on, so a lane that failed over mid-suite yields one row per model
// actually used instead of crediting every case to whichever model happened to serve
// the first one. Keying by the LANE (the agent name) is what silently attributed 9 of
// Run B's 17 cases to a model that never saw them.
//
// Consequence, accepted: two lanes that realize the same model AND the same persona
// merge into a single row, where the lane-keyed accumulator emitted two rows of
// identical public identity. reviewerPersona falls back to the agent name when no
// persona is configured, so distinct lanes keep distinct personas by default.
type reviewerKey struct {
	model   string
	persona string
}

// scoredRow pairs one row's PUBLIC (post-scrub) identity with the accumulator behind
// it, so the reviewer rows and the coverage rows are emitted from a single ordered
// sequence and cannot fall out of alignment.
type scoredRow struct {
	id  reviewerKey
	acc *reviewerAcc
}

// sortScoredRows orders rows ascending by (model, persona) so aggregation never
// depends on map iteration — determinism is an acceptance criterion here, not a
// nicety (the reproducibility contract requires byte-identical output for identical
// input). It matches Score's own sort, so the accumulator hands Score a slice that
// is already in the order Score will keep.
func sortScoredRows(rows []scoredRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].id.model != rows[j].id.model {
			return rows[i].id.model < rows[j].id.model
		}
		return rows[i].id.persona < rows[j].id.persona
	})
}

// reviewerAcc accumulates one realized identity's outcomes across every case. Model
// and persona are NOT stored here — they are the map key, so there is no second copy
// that could drift from it.
type reviewerAcc struct {
	cases         []benchmark.CaseScore
	caseIDs       []string       // suite case ids this identity actually scored, in case order
	outcomes      map[string]int // benchmark.Outcome* -> number of this identity's cases
	fallbackCases int            // cases fanout served from a fallback rather than the primary
	// agents records which LANES folded into this identity, purely so the
	// duplicate-case diagnostic can name the colliding lanes. The accepted
	// raw-identity merge (two lanes, same model AND persona, DISJOINT case sets) is
	// unaffected by this bookkeeping.
	agents    map[string]struct{}
	costUSD   float64
	latencies []int64 // per-case wall-clock, recorded only when usage was reported
}

// reviewerOutcome classifies what actually happened when one reviewer met one case,
// reading signals fanout already computed and stamped onto the AgentStatus. Nothing
// here re-derives them: UnparseableResponse in particular encodes a decision about
// the clean-review sentinel (stream.IsNoFindings) that must not be re-implemented
// against raw content, because excluding the sentinel is exactly what preserves the
// clean-vs-garbage distinction.
//
// PRECEDENCE — failed > unparseable > truncated > incomplete > findings > clean.
// The signals are not mutually exclusive on the wire (a truncated response can also
// raise findings; a failed slot has no findings either way), so the order is a
// decision rather than an implication, and this switch is its single statement of
// record.
//
// Data-integrity signals outrank volume signals throughout. A truncated response that
// raised five findings reports "truncated", not "findings", because the
// incompleteness is the load-bearing fact about that row — the five categories it did
// raise are still recorded in the score, so nothing is lost by saying so. A reviewer
// whose INPUT was cut short reports "incomplete" for the same reason: it may have
// raised nothing, but only about the fraction it read. Both routes to a partial input
// map to that one value — a chunked persona whose bins failed (UnreviewedChunks) and a
// byte-budget shed of the payload itself (Truncated, with FilesDropped naming the
// shed entries by path). Reusing OutcomeIncomplete rather than minting a new value is
// deliberate: the vocabulary is fail-closed at the checkpoint and coverage trust
// boundaries, so an older binary reading a newer run's outcome must find a value it
// already knows.
func reviewerOutcome(a fanout.AgentStatus, raised []string) string {
	switch {
	case a.Status != fanout.StatusOK || a.Error != "":
		return benchmark.OutcomeFailed
	case a.UnparseableResponse:
		return benchmark.OutcomeUnparseable
	case a.ResponseTruncated:
		return benchmark.OutcomeTruncated
	case a.UnreviewedChunks > 0 || a.Truncated:
		return benchmark.OutcomeIncomplete
	case len(raised) > 0:
		return benchmark.OutcomeFindings
	default:
		return benchmark.OutcomeClean
	}
}

// reviewerCaseOutcome is everything one reviewer produced on one case: the realized
// identity that served it, which case it was, the score inputs, and the usage-gated
// cost/latency contribution. It is a struct rather than a positional argument list
// because the fold path is shared by fresh execution and checkpoint replay — two call
// sites that must stay in step, and that a nine-argument signature makes easy to
// silently transpose.
type reviewerCaseOutcome struct {
	model         string
	persona       string
	caseID        string
	expected      []string
	raised        []string
	usageReported bool
	costUSD       float64
	latencyMS     int64
	// outcome is a benchmark.Outcome* value. The empty string is benchmark.OutcomeUnknown
	// — a checkpoint written before the field existed — and must stay distinguishable
	// from OutcomeClean.
	outcome string
	// fallbackUsed records that fanout served this case from a fallback model rather
	// than the slot's configured primary. Tracked alongside outcome, never inside it.
	fallbackUsed bool
	// agent is the lane (configured agent name) that produced this outcome. It plays
	// no part in the fold — only in the duplicate-case diagnostic, which must name
	// the colliding lanes.
	agent string
}

// applyReviewerOutcome folds one reviewer's single-case outcome into the
// accumulator under its REALIZED (model, persona) identity, creating the accumulator
// (and registering the key in order) on first sighting of that identity. It is the
// SINGLE fold path shared by fresh execution and checkpoint replay, so a resumed run
// reconstructs accs identically to an uninterrupted one (AC3) — including across a
// failover boundary, because checkpointReviewer.Model already stores the realized
// model rather than the configured one (pinned end to end by
// TestExecuteBenchmarkRun_ResumeAcrossFailoverBoundarySplitsCoverage).
//
// The identity is the map KEY, so a case can never be folded under a model that did
// not serve it. The previous lane-keyed version locked model/persona at first
// sighting, which is precisely how a mid-suite failover ended up publishing another
// model's work under the primary's name.
//
// Two lanes realizing the SAME identity with DISJOINT case sets merge into one row
// — the accepted design (see reviewerKey's doc). But a repeated (key, caseID) means
// two lanes scored the SAME case under one identity: the merged row would list the
// case twice, doubling Runs past the suite size and failing the export gate as
// "malformed" — a legitimate run destroyed and misdiagnosed as tampering. That
// shape is rejected here, at the fold, with the colliding lanes named.
//
// Cost and latency are still accumulated in case order within each key, so the float
// sum and the latency median stay byte-identical to an uninterrupted run.
func applyReviewerOutcome(accs map[reviewerKey]*reviewerAcc, order *[]reviewerKey, o reviewerCaseOutcome) error {
	key := reviewerKey{model: o.model, persona: o.persona}
	acc := accs[key]
	if acc == nil {
		acc = &reviewerAcc{agents: map[string]struct{}{}}
		accs[key] = acc
		*order = append(*order, key)
	} else if slices.Contains(acc.caseIDs, o.caseID) {
		lanes := make([]string, 0, len(acc.agents)+1)
		for a := range acc.agents {
			lanes = append(lanes, a)
		}
		lanes = append(lanes, o.agent)
		sort.Strings(lanes)
		// %q on the identity and the lanes, not %s: o.model is the provider/proxy-reported
		// realized model (reviewerModel), the same untrusted class stripTerminalControlRunes
		// defends elsewhere, and cobra prints this RunE error straight to the terminal.
		// %q is the verb this package already chose for comparison and identity messages —
		// terminal-safe AND legible, unlike stripping, which sanitizes by deletion.
		return fmt.Errorf("case %q scored twice under realized identity %q/%q (lanes %q); "+
			"two lanes realizing the same (model, persona) must partition the suite, not both score it",
			o.caseID, o.model, o.persona, strings.Join(lanes, ", "))
	}
	if acc.agents == nil {
		acc.agents = map[string]struct{}{}
	}
	acc.agents[o.agent] = struct{}{}
	acc.cases = append(acc.cases, benchmark.CaseScore{Expected: o.expected, Raised: o.raised})
	acc.caseIDs = append(acc.caseIDs, o.caseID)
	if acc.outcomes == nil {
		acc.outcomes = map[string]int{}
	}
	acc.outcomes[benchmark.OutcomeTallyKey(o.outcome)]++
	if o.fallbackUsed {
		acc.fallbackCases++
	}
	if o.usageReported {
		acc.costUSD += o.costUSD
		acc.latencies = append(acc.latencies, o.latencyMS)
	}
	return nil
}

// replayCheckpointCase folds a checkpointed case's recorded per-reviewer outcomes
// back into the accumulator via the same applyReviewerOutcome path the fresh loop
// uses — no review re-execution and no Completer call (AC2). Expected categories
// are re-read from the suite manifest (passed in as expected) because they are
// identical for every reviewer of a case and are not durable per-reviewer state.
func replayCheckpointCase(accs map[reviewerKey]*reviewerAcc, order *[]reviewerKey, entry checkpointCase, expected []string) error {
	for _, r := range entry.Reviewers {
		if err := applyReviewerOutcome(accs, order, reviewerCaseOutcome{
			model:         r.Model,
			persona:       r.Persona,
			caseID:        entry.CaseID,
			expected:      expected,
			raised:        r.Raised,
			usageReported: r.UsageReported,
			costUSD:       r.CostUSD,
			latencyMS:     r.LatencyMS,
			// A checkpoint written before the outcome field existed decodes to the
			// empty string — benchmark.OutcomeUnknown — and is folded as such. It must
			// not be inferred from Raised: "no categories recorded" is exactly the
			// ambiguity the vocabulary exists to resolve, so guessing "clean" here
			// would manufacture the claim the epic set out to stop making.
			outcome:      r.Outcome,
			fallbackUsed: r.FallbackUsed,
			agent:        r.Agent,
		}); err != nil {
			return fmt.Errorf("replaying checkpointed case %q: %w", entry.CaseID, err)
		}
	}
	return nil
}

// readCaseFindings parses the merged pool findings.txt for one review and groups
// each finding's category by its REVIEWER (the agent name the engine stamped,
// never a model-supplied value). A pool with no findings yields an empty map.
func readCaseFindings(reviewDir string) (map[string][]string, error) {
	path := filepath.Join(reviewDir, "sources", "pool", "findings.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]string{}, nil
		}
		return nil, err
	}
	parsed, err := stream.ParseSource(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(parsed.Findings))
	for _, f := range parsed.Findings {
		out[f.Reviewer] = append(out[f.Reviewer], f.Category)
	}
	// A SKIPPED row is a finding the reviewer really emitted whose columns the
	// parser could not align (an unescaped pipe in PROBLEM is the usual cause).
	// Dropping it here would shrink the out-of-vocabulary DENOMINATOR, so the
	// reviewer producing the worst-formed output would earn the best drift rate —
	// the metric would reward exactly the behaviour it exists to detect. Fold each
	// one in with an EMPTY category, which counts as drift by the same rule that
	// already governs an empty CATEGORY column.
	//
	// REVIEWER is the engine's last-appended column, so the final field survives an
	// overflow earlier in the row. parse() strips trailing empty fields BEFORE
	// classifying a row as skipped, so that field is non-empty by construction;
	// mirror the strip here to land on the same one. An unrecognized reviewer name
	// keys a map entry no agent reads, exactly as an unrecognized REVIEWER on a
	// well-formed row already does.
	for _, s := range parsed.Skipped {
		fields := strings.Split(s.Content, "|")
		for len(fields) > 1 && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		reviewer := fields[len(fields)-1]
		out[reviewer] = append(out[reviewer], "")
	}
	return out, nil
}

// reviewerModel resolves a reviewer's model id, preferring the usage-reported
// value in the pool summary and falling back to the configured model when the
// provider reported no usage (e.g. a stub completer leaves AgentStatus.Model empty).
//
// FALLBACK OUTRANKS THE USAGE-REPORTED MODEL when the two disagree. They can only
// disagree on the chunked path: fanout stamps Result.Model from the invocation that
// actually ran, so a wholly-failed-over slot reports the SAME model in both fields,
// but mergeResultGroup builds a chunked slot's merged result as `out := g[0]` and
// never recomputes Model while unioning FallbackUsed and taking a modal
// FallbackModel (internal/fanout/chunker.go). A slot whose chunks partly fell back
// therefore arrives here carrying chunk 0's model beside another model's
// FallbackUsed — and preferring Model would publish the whole case, and its summed
// token cost, under a model that served only part of it. Since chunked is the
// shipped review_strategy, that is the ordinary path, not a corner of it.
//
// A mixed-chunk case cannot be attributed EXACTLY without a per-chunk breakdown the
// merge does not keep, so this is the least-wrong answer rather than a precise one:
// FallbackUsed is the durable signal that the primary did not serve all of this, so
// the primary is the single answer known to be false. Recovering exact attribution
// needs fanout to carry per-chunk models through the merge — until then the row is
// credited to the model that displaced the primary.
//
// The remaining FallbackModel step covers a case that FAILED after the slot had
// already failed over: no usage was returned, so no usage-reported model was
// stamped, and resolving straight to the registry would credit the configured
// primary — a model that by definition did not serve the case.
//
// Every non-failover path is unchanged: FallbackUsed is false, so resolution is the
// original prefer-usage-then-registry pair.
func reviewerModel(cfg *fanout.ReviewConfig, a fanout.AgentStatus) string {
	if a.FallbackUsed && a.FallbackModel != "" {
		return a.FallbackModel
	}
	if a.Model != "" {
		return a.Model
	}
	return cfg.Registry.Agents[a.Agent].Model
}

// reviewerPersona resolves a reviewer's persona from the registry, falling back to
// the agent name when no persona is configured.
func reviewerPersona(cfg *fanout.ReviewConfig, agent string) string {
	if p := cfg.Registry.Agents[agent].Persona; p != "" {
		return p
	}
	return agent
}

// medianInt64 returns the p50 of vs; 0 for an empty slice, so a no-usage run
// reports a deterministic 0 latency. It uses the SAME definition as
// scorecard.medianInt64 (odd: the middle element; even: floor of the two middles)
// so the shared public latency_p50_ms column is computed identically for benchmark
// and production rows on the leaderboard. lo + (hi-lo)/2 is the overflow-safe form
// of floor((lo+hi)/2).
func medianInt64(vs []int64) int64 {
	n := len(vs)
	if n == 0 {
		return 0
	}
	sorted := make([]int64, n)
	copy(sorted, vs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if n%2 == 1 {
		return sorted[n/2]
	}
	lo, hi := sorted[n/2-1], sorted[n/2]
	return lo + (hi-lo)/2
}
