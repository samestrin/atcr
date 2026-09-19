package reconcile

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/benchmark.md's "Running a `repo-state-v1` suite" section documents the second
// benchmark tier: where its metric lives, what its JSON keys are called, and which
// standard-v1 behaviours it deliberately does NOT share. Nothing pinned any of it.
//
// That matters more here than for most sections, because this doc replaced a
// paragraph that had gone FALSE — it said `benchmarks/repo-state-v1/` was not runnable,
// which stopped being true the moment the loader landed. A doc that has already drifted
// once, corrected in prose only, will drift again just as silently.
//
// The guard is BIDIRECTIONAL, like its sibling benchmark_publishable_doc_test.go:
// asserting only that the doc names a key catches a doc edit but not a code edit, and
// the code direction is the one that misleads a reader — the doc keeps promising a
// field the run-result no longer emits. Each claim is therefore asserted twice, once
// against the doc and once against the struct tag or code arm that makes it true.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift tests
// (no Go package lives under docs/, and the published reconcile/ module must not assume
// this repo's file layout). See justification_record_boundary_test.go for the precedent.
func TestBenchmarkDoc_RepoStateSectionMatchesTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	score := readRepoFile(t, "../../internal/benchmark/score_repostate.go")
	runResult := readRepoFile(t, "../../internal/benchmark/benchmark.go")
	cli := readRepoFile(t, "../../cli/benchmark_repostate.go")

	// The array's own key. The doc tells a reader where to look in the run-result;
	// a renamed struct tag would leave them looking for a key that is not there.
	assert.Contains(t, doc, "reviewer_positional_recall",
		"the doc must name the run-result array that carries the metric")
	assert.Contains(t, runResult, `json:"reviewer_positional_recall,omitempty"`,
		"RunResult must still emit the array the doc names")

	// The three rates, each documented as a row of a table. A reader copies these
	// names into a jq expression, so a drifted name is a silently empty result.
	// Matched as a BACKTICKED table cell, not a bare substring: "recall" is a
	// substring of the other two keys and appears in at least eight unrelated
	// sentences in this doc, so a bare-word assertion verified nothing about the
	// recall row — a renamed or deleted cell would have passed untouched.
	for _, key := range []string{"recall", "outside_diff_recall", "within_diff_recall"} {
		assert.Contains(t, doc, "`"+key+"`", "the doc must carry %q as a table cell", key)
		assert.Contains(t, score, `json:"`+key+`,omitempty"`,
			"ReviewerPositionalRecall must still emit %q, with omitempty — the doc promises an "+
				"ABSENT key rather than a zero when nothing was expected", key)
	}

	// The unmeasured-vs-zero rule. This is the claim a consumer acts on when deciding
	// whether a 0 means "missed everything" or "nothing was planted", so the doc saying
	// it and the code doing it must not come apart.
	//
	// Anchored on SEMANTIC substrings, not gofmt-level formatting: the previous pin
	// matched "if expected <= 0 {\n\t\treturn nil\n\t}" — indentation and newlines
	// included — so any reflow of rate() broke a test that has nothing to do with
	// docs. "expected <= 0" and "return nil" are individually gofmt-stable and
	// together still pin the nil-when-unmeasured arm. The behavior itself is not
	// executable from this module (rate() is unexported in the parent module's
	// internal tree), so the source-semantics anchor is the strongest pin available
	// here; the executable half belongs to internal/benchmark's own test suite.
	assert.Contains(t, doc, "an unmeasured rate must not read as a measured zero",
		"the doc must state the unmeasured-vs-zero rule")
	assert.Contains(t, score, "expected <= 0",
		"rate() must still gate on the expected denominator")
	assert.Contains(t, score, "return nil",
		"rate() must still return nil rather than 0 when nothing was expected")

	// --checkpoint is REFUSED, not ignored. A doc that said "ignored" while the code
	// refused would be merely wrong; a doc that says "refused" while the code silently
	// ignores would cost an operator a whole panel run.
	assert.Contains(t, doc, "`--checkpoint` is rejected, not ignored",
		"the doc must state that the flag is refused")
	// Anchored on the message's stable prefix, not the %s verb: the refusal's
	// suite-format interpolation is an implementation detail, and pinning the raw
	// format string broke on any reword that kept the operator-visible sentence.
	assert.Contains(t, cli, "--checkpoint is not supported for a",
		"checkRepoStateFlags must still return an error for the flag")

	// The grounding gate stays ON. This is the design decision the whole tier's
	// headline number depends on, and the one a future reader is most likely to
	// mistake for an oversight and "fix".
	assert.Contains(t, doc, "grounding gate stays ON",
		"the doc must record that the Epic 14.1 gate is deliberately left enabled")
	assert.Contains(t, cli, "THE EPIC 14.1 GROUNDING GATE STAYS ON",
		"the runner must still carry the rationale the doc points at")

	// The range path, not the diff path. A future refactor routing repo-state cases
	// through PrepareReviewFromDiff would silently stop measuring the two sibling
	// epics this tier exists to measure, and the doc would still claim otherwise.
	assert.Contains(t, doc, "It reviews a real git range, not an ingested diff",
		"the doc must state which review path the tier uses")
	assert.Contains(t, cli, "fanout.PrepareReview(ctx, cfg, req)",
		"the runner must still drive the RANGE path")
	// Matched as a CALL — "fanout.PrepareReviewFromDiff(" — not as a bare mention.
	// The runner's own doc comment names the function it deliberately does not use,
	// explaining why, and a bare NotContains would forbid that explanation.
	assert.NotContains(t, cli, "fanout.PrepareReviewFromDiff(",
		"routing repo-state cases through diff ingestion would drop the RangeBuilder, "+
			"and with it the claim ledger and pre-fetching this tier measures")

	// The SEVENTH claim, and the one this section previously got wrong in the other
	// direction: the doc said `verify` and `export` "remain standard-v1-only" while
	// export already accepted a repo-state run-result. Both subcommands now route the
	// tier, and each half is pinned to the code arm that makes it true — an unpinned
	// sentence about tier support is exactly the one that drifted before.
	cmd := readRepoFile(t, "../../cli/benchmark.go")
	coverage := readRepoFile(t, "../../cli/benchmark_coverage.go")

	assert.Contains(t, doc, "`atcr benchmark verify` and `atcr benchmark export` route both tiers",
		"the doc must state that verify and export are no longer standard-v1-only")
	assert.Contains(t, cmd, "return verifyRepoStateSuite(cmd, suitePath)",
		"runBenchmarkVerify must still route the repo-state arm the doc promises")
	assert.Contains(t, coverage, "func loadSuiteAnchor(",
		"the export denominator anchor must still have a tier-aware load, or --suite-path "+
			"cannot anchor the repo-state run-result the doc says it can")

	// verify prints NO reproducibility hash for this tier. A reader who expects one
	// and does not get it must find that documented rather than read it as a bug.
	assert.Contains(t, doc, "prints no reproducibility hash for it",
		"the doc must record that verify emits no repro hash on the repo-state arm")
	assert.Contains(t, cmd, "not defined for %s (standard-v1 only)",
		"verifyRepoStateSuite must still name the omission instead of printing nothing")
}

// The stale claim this section replaced must not come back. It is asserted by
// ABSENCE because that is how it would return: a future editor restoring an old
// paragraph, or copying the caveat from a pre-35.16.10 revision.
func TestBenchmarkDoc_NoLongerClaimsRepoStateIsUnrunnable(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")

	for _, stale := range []string{
		"no loader reads them yet",
		"neither validates nor runs it",
	} {
		assert.NotContains(t, doc, stale,
			"docs/benchmark.md still claims repo-state-v1 is not runnable, which stopped "+
				"being true when the loader landed in epic 35.16.10")
	}
}

// readRepoFileOrSkip is deliberately absent: a missing file is a real failure here,
// not a reason to skip. Every path above is tracked in this repository.
var _ = os.ReadFile

// The shipped suite must actually contain the documents this epic's AC6 requires.
// A SPOT-CHECK.md that was never written is the failure mode AC6 exists to prevent,
// and it is invisible to every other test in the tree.
//
// AC6 is "every case hand-verified in SPOT-CHECK.md" — existence and non-emptiness
// do not deliver it: a file holding a single byte passed the old check, a fifth
// case added to the manifest with no spot-check section passed, and a suite_version
// bump stranding the doc's quoted version passed. Each case id must therefore
// appear as a RESULTS heading, and the doc's title must quote the manifest's
// CURRENT suite_version (the historical run block legitimately still quotes the
// version the run was recorded at, so the anchor is the title, not the whole file).
func TestRepoStateSuite_ShipsItsVerificationDocuments(t *testing.T) {
	for _, path := range []string{
		"../../benchmarks/repo-state-v1/SPOT-CHECK.md",
		"../../benchmarks/repo-state-v1/NOTICE.md",
	} {
		data, err := os.ReadFile(path)
		require.NoError(t, err, "the suite must ship %s", path)
		assert.NotEmpty(t, data)
	}

	manifestData, err := os.ReadFile("../../benchmarks/repo-state-v1/suite.json")
	require.NoError(t, err)
	var manifest struct {
		Suite        string `json:"suite"`
		SuiteVersion string `json:"suite_version"`
		Cases        []struct {
			ID  string `json:"id"`
			Dir string `json:"dir"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(manifestData, &manifest))
	require.NotEmpty(t, manifest.Cases, "the manifest must declare its cases")

	spot, err := os.ReadFile("../../benchmarks/repo-state-v1/SPOT-CHECK.md")
	require.NoError(t, err)
	doc := string(spot)

	// Every manifest case must have its own hand-verification heading (AC6's core).
	for _, c := range manifest.Cases {
		assert.Contains(t, doc, "### `"+c.ID+"`",
			"SPOT-CHECK.md must carry a results heading naming case %q — a case with no "+
				"section was never hand-verified, which is exactly what AC6 forbids", c.ID)
	}

	// The title's quoted version must equal the manifest's, so a suite_version bump
	// cannot strand the spot-check at a stale number unnoticed.
	titleRe := regexp.MustCompile("# Spot-check — `repo-state-v1` ([0-9]+\\.[0-9]+\\.[0-9]+)")
	m := titleRe.FindStringSubmatch(doc)
	require.NotNil(t, m, "SPOT-CHECK.md title must quote the suite version it was checked at")
	assert.Equal(t, manifest.SuiteVersion, m[1],
		"SPOT-CHECK.md was checked at %s but the manifest is now %s — re-run the hand-check "+
			"or bump the doc before shipping the suite at a version its verification does not cover",
		m[1], manifest.SuiteVersion)
}

// The partial-run contract is the one claim in this section whose reader is making a
// COST decision: "will a late failure forfeit the ten minutes of paid panel work I
// already spent?" A doc that still describes the old all-or-nothing behaviour costs
// them a re-run they did not need, and a doc that promises the new behaviour after
// the code reverted costs them the run itself. Pinned bidirectionally for the same
// reason as its siblings above.
func TestBenchmarkDoc_RepoStatePartialRunContractMatchesTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	runResult := readRepoFile(t, "../../internal/benchmark/benchmark.go")
	cli := readRepoFile(t, "../../cli/benchmark_repostate.go")
	coverage := readRepoFile(t, "../../cli/benchmark_coverage.go")

	// The channel's own key, so a reader knows where in the run-result to look.
	assert.Contains(t, doc, "`case_failures`",
		"the doc must name the run-result array that records unmeasured cases")
	assert.Contains(t, runResult, `json:"case_failures,omitempty"`,
		"the run-result must still emit the key the doc names")

	// The field's back-compat contract. The omitempty tag makes a clean run
	// serialize identically to a pre-field run-result, and both unmarshal to nil —
	// the reason a consumer that never heard of the channel still parses a partial
	// run. That contract lived only in the field's comment, unenforced by any test;
	// pinned here so an edit that silently drops the omitempty (which would emit
	// `"case_failures":null` on every clean run and break byte-identity) fails this
	// guard instead of shipping as a silent schema change.
	//
	// Whitespace-normalized before matching because the sentence wraps across
	// comment lines in the source: comment continuation markers (`\n// `) are
	// folded into spaces first, then runs of whitespace collapse, so a reflow that
	// keeps the words keeps the guard green. The phrase is unique to the
	// CaseFailures comment — the Vocabulary field's similar sentence continues
	// with an em-dash ("existed — and both"), not this comma.
	joined := regexp.MustCompile(`\n\s*//\s?`).ReplaceAllString(runResult, " ")
	compactRunResult := regexp.MustCompile(`\s+`).ReplaceAllString(joined, " ")
	assert.Contains(t, compactRunResult,
		"omitempty so a clean run serializes identically to a run-result written before this field existed, and both unmarshal to nil.",
		"the CaseFailures field comment must keep its omitempty/back-compat contract — "+
			"it is the only statement of why a clean run stays byte-identical to a pre-field run-result")

	// The distinction the whole design rests on: unmeasured, not missed. A reader
	// who takes a failed case for a zero would mis-read every recall on the run.
	//
	// Anchored to the SENTENCE, not the bare word. "unmeasured" already appears in
	// eight paragraphs of this doc that predate the partial-run section, so deleting
	// the section outright left this assertion green — verifying nothing about the
	// claim it names. The sibling at the top of this file states the rule this
	// violated: match a distinctive phrase, never a word the doc says elsewhere.
	assert.Contains(t, doc, "is **unmeasured**, not missed",
		"the doc must say a failed case is unmeasured rather than scored as a miss")
	assert.Contains(t, cli, "recorded as unmeasured and skipped",
		"the runner must still skip the failed case rather than score it")

	// The one execution failure that still aborts — AC5. A doc claiming a total-roster
	// failure is survivable would invite publishing a run whose missing case came from
	// a whole-provider outage.
	assert.Contains(t, doc, "total-roster failure still aborts",
		"the doc must keep the all-agents-failed abort's exception explicit")
	assert.Contains(t, cli, "errors.Is(err, fanout.ErrAllAgentsFailed)",
		"the runner must still propagate a total-roster failure rather than record it")

	// The empty roster aborts for the OPPOSITE reason — a deterministic configuration
	// defect, not a transient outage — so the doc must give it its own row and its own
	// rationale rather than folding it into the total-roster failure's justification.
	assert.Contains(t, doc, "deterministic configuration defect",
		"the doc must state the empty-roster abort's own rationale, not borrow the outage's")
	assert.Contains(t, cli, "fanout.ErrEmptyRoster",
		"the runner must still propagate an empty-roster execution failure rather than record it")

	// Retention on a partial run — AC4. This is the sentence that tells an operator
	// their paid artifacts are recoverable instead of gone.
	assert.Contains(t, doc, "work dir is retained",
		"the doc must state that a partial run keeps its work dir")
	assert.Contains(t, cli, "benchmark work dir retained after a partial run",
		"the runner must still retain and report the work dir on a partial run")

	// The export gate is still closed by default on a partial run: a recorded failure
	// EXPLAINS a shortfall, it does not excuse one.
	// Same rule: "does not excuse" is a three-word fragment with no anchor to the
	// section it is supposed to pin. The full clause names both halves of the claim.
	assert.Contains(t, doc, "**explains** a coverage shortfall; it **does not excuse** one",
		"the doc must say a recorded failure explains but does not waive the coverage gate")
	assert.Contains(t, coverage, "re-run the missing or unmeasured cases",
		"the gate must still reject a short run-result by default")
}

// The doc's prose enumerates the failure stages, and the export gate names four
// rejection arms. Neither was pinned: renaming a reason constant, or rewording a
// rejection literal, left the doc stating a falsehood with this file green — the
// one outcome the header above says these guards exist to prevent. The vocabulary
// is extracted from case_failure.go rather than transcribed, so a renamed constant
// (or a newly added one) fails here until the doc names it as the code writes it.
func TestBenchmarkDoc_CaseFailureReasonVocabularyMatchesTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	caseFailures := readRepoFile(t, "../../internal/benchmark/case_failure.go")
	coverage := readRepoFile(t, "../../cli/benchmark_coverage.go")

	// Every wire value the producer can write, the doc must name — backticked, so a
	// prose word that merely resembles one ("prepared" vs `prepare`) does not count.
	reasons := regexp.MustCompile(`CaseFailure\w+\s*=\s*"([^"]+)"`).FindAllStringSubmatch(caseFailures, -1)
	require.NotEmpty(t, reasons, "case_failure.go must still declare the reason constants")
	for _, reason := range reasons {
		assert.Containsf(t, doc, "`"+reason[1]+"`",
			"the doc must name the %q failure reason as the code writes it", reason[1])
	}

	// The export gate's four rejection arms, verbatim — the doc quotes the gate's
	// behaviour, so a reworded arm must fail here until the doc agrees with it.
	for _, literal := range []string{
		"outside the failure vocabulary",
		"suite_case_ids does not declare",
		"both scored and infrastructure-failed",
		"more than once",
	} {
		assert.Containsf(t, coverage, literal,
			"the export gate must keep the %q rejection arm the doc describes", literal)
	}
}

// abortClasses is the abort taxonomy's ONE enumeration. Every failure that still
// kills a repo-state run appears here exactly once, with the code marker that
// implements it and the phrase each prose copy must use to name it.
//
// The taxonomy had forked into four hand-kept copies — this runner, the
// docs/benchmark.md abort table, the CHANGELOG paragraph, and case_failure.go's
// "one failure site per reason, one to one" claim — and the copies already
// disagreed on PARTITIONING: the changelog merged the empty roster into the
// total-roster row while the code treats it as a distinct second sentinel with the
// opposite rationale. Nothing failed when they diverged, so reclassifying a failure
// site meant editing N prose sites in lockstep and hoping.
//
// Keying the table on the CODE MARKER is what makes this a single source of truth
// rather than a fifth copy: a new abort with no entry here has no marker to match,
// and an entry whose marker is deleted fails immediately.
var abortClasses = []struct {
	name      string // what this test calls it, for the failure message
	codeMark  string // the arm in cli/benchmark_repostate.go that aborts
	docPhrase string // how docs/benchmark.md's abort table names it
	logPhrase string // how the CHANGELOG paragraph names it
}{
	{"total-roster failure", "errors.Is(err, fanout.ErrAllAgentsFailed)", "total-roster failure still aborts", "total-roster failure"},
	{"empty roster", "fanout.ErrEmptyRoster", "**empty roster**", "empty-roster failure"},
	{"unwinnable expectation", "benchmark.ValidateAgainstHead(c, mc.Root)", "**unwinnable expectation**", "unwinnable expectation"},
	{"scored-twice / identity collision", "scored twice under realized identity", "**scored-twice identity guard**", "scored-twice and identity-collision guards"},
	{"cancellation", "benchmark run cancelled after", "**Cancellation** (SIGINT/SIGTERM)", "cancellation (SIGINT/SIGTERM)"},
	{"nothing scored", "no case could be scored", "**Nothing scored at all.**", "run that scored nothing at all"},
}

// The abort taxonomy must partition identically in the runner, the doc table and the
// changelog. Counting the doc's table ROWS (rather than only matching phrases) is what
// catches the drift direction that actually happened: a row added to the doc and never
// mirrored into the changelog reads to a release-notes reader as a shorter list of
// fatal failures than the tool really has.
func TestBenchmarkDoc_AbortTaxonomyPartitionsIdenticallyEverywhere(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	cli := readRepoFile(t, "../../cli/benchmark_repostate.go")
	changelog := readRepoFile(t, "../../CHANGELOG.md")

	for _, class := range abortClasses {
		assert.Containsf(t, cli, class.codeMark,
			"the runner must still abort on %s — this table is the taxonomy's source of truth", class.name)
		assert.Containsf(t, doc, class.docPhrase,
			"the doc's abort table must name %s", class.name)
		assert.Containsf(t, changelog, class.logPhrase,
			"the CHANGELOG must name %s; a class the doc lists and the changelog merges away "+
				"is the partitioning fork this guard exists to stop", class.name)
	}

	// The doc table's row count, read off the rendered table rather than trusted from
	// the phrase matches above: an EXTRA row (a class the code no longer has, or one
	// nobody added here) is invisible to a per-class Contains.
	rows := 0
	inTable := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| Failure | Why it aborts |") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		rows++
	}
	assert.Equal(t, len(abortClasses), rows,
		"docs/benchmark.md's abort table must have exactly one row per abort class")

	// The changelog states the count in words. A numeral that disagrees with the table
	// is the cheapest possible way for the two copies to fork again.
	assert.Contains(t, changelog, "Six failure classes still abort the whole run",
		"the CHANGELOG's count must match the abort table's row count")
}

// The export rejection is emitted as ONE line by checkCoverage's fmt.Errorf, but the
// doc rendered it as a three-line terminal block nobody will ever see. Pin the real
// shape: prefix, shortfall row and remedy sentence on a single doc line, in the order
// the format string writes them.
func TestBenchmarkDoc_ExportRejectionExampleMatchesTheEmittedLine(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	coverage := readRepoFile(t, "../../cli/benchmark_coverage.go")

	assert.Contains(t, coverage, "has reviewer row(s) scored over less than the full %d-case suite: %s; ",
		"the gate's format string must keep the shape the doc quotes")
	onOneLine := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.Contains(line, "has reviewer row(s) scored over less than the full") &&
			strings.Contains(line, "re-run the missing or unmeasured cases") {
			onOneLine = true
			break
		}
	}
	assert.True(t, onOneLine,
		"the doc's export-rejection example must be one line, as the gate emits it — not a wrapped three-line block nobody will see")
}
