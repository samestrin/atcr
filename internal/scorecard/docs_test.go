package scorecard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocs_ScorecardMdExists asserts the user-facing reference doc
// (docs/scorecard.md) is present at the repository root (AC 06-01). The repo
// root is located by walking up from the test's working directory to the
// directory containing go.mod, so the test is independent of where it runs.
func TestDocs_ScorecardMdExists(t *testing.T) {
	root := repoRoot(t)
	docPath := filepath.Join(root, "docs", "scorecard.md")
	info, err := os.Stat(docPath)
	if err != nil {
		t.Fatalf("docs/scorecard.md not found at %s: %v", docPath, err)
	}
	if info.IsDir() {
		t.Fatalf("docs/scorecard.md is a directory, expected a file: %s", docPath)
	}
	if info.Size() == 0 {
		t.Fatalf("docs/scorecard.md is empty: %s", docPath)
	}
}

// repoRoot walks up from the current working directory until it finds the
// directory containing go.mod (the module root).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from working directory")
		}
		dir = parent
	}
}

// TestDocs_ScorecardMdRaisedDenominator pins the record-schema table against the
// era discriminator this package writes: Record.RaisedDenominator supersedes the
// RaisedIncludesUnresolved bool, so the doc must carry a `raised_denominator`
// row and describe `raised_includes_unresolved` as superseded-but-retained —
// a reader of the table must not conclude the bool still carries the era alone.
func TestDocs_ScorecardMdRaisedDenominator(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "scorecard.md"))
	if err != nil {
		t.Fatalf("read docs/scorecard.md: %v", err)
	}
	doc := string(data)
	if !strings.Contains(doc, "| `raised_denominator` | int | conditional |") {
		t.Errorf("record-field table is missing the raised_denominator row that Record.RaisedDenominator (scorecard.go) writes")
	}
	if !strings.Contains(doc, "Superseded but retained") {
		t.Errorf("raised_includes_unresolved row must be described as superseded-but-retained now that raised_denominator carries the era")
	}
}

// TestDocs_ScorecardMdSchemaVersionMatchesTheConstant is the pin the v1->v2 bump
// needed and did not have. docs/scorecard.md states the current version in four
// places, and a bump that updates the constant while leaving the doc claiming
// "Currently 1" publishes a reference that is simply false — the exact drift the
// sibling pins in this file exist to stop.
func TestDocs_ScorecardMdSchemaVersionMatchesTheConstant(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	for _, want := range []string{
		fmt.Sprintf("## Record Schema (v%d)", SchemaVersion),
		fmt.Sprintf(`"schema_version": %d,`, SchemaVersion),
		fmt.Sprintf("| `schema_version` | int | always | Record schema version. Currently `%d`. |", SchemaVersion),
		fmt.Sprintf("- `schema_version` (`%d`) is stamped on every **stored** record", SchemaVersion),
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md has drifted from SchemaVersion = %d; missing:\n%s",
				SchemaVersion, want)
		}
	}
}

// TestDocs_ScorecardMdDocumentsOutcomeAndEligibility pins the two fields the v2
// bump carries and the trust rule that reads the first of them. The eligibility
// rule is the one that changes what an operator SEES — a pre-v2 store renders an
// all-n/a `personas list --scores` table — so an undocumented version of it is a
// support problem, not a cosmetic gap.
func TestDocs_ScorecardMdDocumentsOutcomeAndEligibility(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	for _, want := range []string{
		"| `outcome` | string | conditional |",
		"| `categories_raised` | array of string | conditional |",
		"- **Outcome eligibility.**",
		"every reviewer in it is excluded and the",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md is missing required passage:\n%s", want)
		}
	}

	// Pin the doc's OWN eligibility sentence against the code allowlist, not the
	// whole file. A whole-file substring search cannot fail: all nine vocabulary
	// values already appear backticked somewhere in this document, so adding
	// `truncated` to eligibleOutcomeRuns would leave the check green while the
	// doc went on stating that truncated is excluded.
	//
	// The sentence lists the counted set and then the excluded set, so asserting
	// each value appears on the correct SIDE of "are excluded" makes the two
	// lists a real complement of each other.
	countedStart := strings.Index(doc, "- **Counted:**")
	excludedStart := strings.Index(doc, "- **Excluded:**")
	if countedStart < 0 || excludedStart <= countedStart {
		t.Fatalf("docs/scorecard.md must render the eligibility split as a Counted: bullet followed by an Excluded: bullet; the pin below cannot work otherwise")
	}
	// Bound the excluded bullet explicitly. strings.Index returns -1 when the
	// bullet is the final block, and slicing on that panics with "slice bounds
	// out of range" — a routine doc edit would crash this test instead of
	// failing it with the diagnostic it exists to print.
	excludedEnd := strings.Index(doc[excludedStart:], "\n\n")
	if excludedEnd < 0 {
		t.Fatalf("docs/scorecard.md: the Excluded: bullet must be followed by a blank line so its extent is unambiguous")
	}
	counted := doc[countedStart:excludedStart]
	excluded := doc[excludedStart : excludedStart+excludedEnd]

	// Each value must be named on the side the CODE actually puts it on, so
	// moving one across the allowlist without moving it in the doc fails here.
	// A whole-file substring search cannot do this: all nine values already
	// appear backticked somewhere in this document.
	for _, o := range allTestOutcomes {
		if o == "" {
			continue // absent/unknown is prose, not a backticked token
		}
		admitted := len(eligibleOutcomeRuns([]Record{{RecordType: RecordTypeReviewer, Outcome: o}})) > 0
		side, other := excluded, counted
		if admitted {
			side, other = counted, excluded
		}
		if !strings.Contains(side, "`"+o+"`") {
			t.Errorf("code treats %q as admitted=%v but docs/scorecard.md does not name it on that side of the eligibility split", o, admitted)
		}
		if strings.Contains(other, "`"+o+"`") {
			t.Errorf("docs/scorecard.md names %q on BOTH sides of the eligibility split", o)
		}
	}
}

// TestDocs_ScorecardMdDocumentsOpportunitySetScoping keeps docs/scorecard.md
// honest about the opportunity-set link.
//
// Two drifts are pinned here because the Phase 3 gate found both. First, the
// `categories_raised` row said "not yet populated" for a whole phase AFTER the
// field started being written on every reviewer record — a reader was told the
// opposite of the truth about a persisted field. Second, the TrustPriors section
// enumerates the filters that decide the denominator and omitted the largest
// change to it, so a reader counting runs would get a different number than the
// code does.
//
// The pins are behavioural, not textual: each asserts the doc names something
// the CODE does, so deleting the behaviour fails the test too.
func TestDocs_ScorecardMdDocumentsOpportunitySetScoping(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	// The field row must not claim the field is unwritten. Scoped to the ROW, not
	// banned document-wide: "not yet populated" is a generic phrase that a future
	// field row could legitimately use, and a whole-file ban would fail that edit
	// with a message pointing at categories_raised.
	row := docTableRow(t, doc, "`categories_raised`")
	assert.NotContains(t, row, "not yet populated",
		"categories_raised is written on every reviewer record; its row must not say otherwise")
	assert.Contains(t, row, "populated since schema 2",
		"the row must state when the field started being written")
	// The routed stream carries the same doc_shield carve-out findings_raised
	// does (reviewerCategories takes the chargeable split). The row twelve lines
	// up documents it precisely; this one must not read as unconditional.
	assert.Contains(t, row, "doc_shield",
		"the routed-stream clause must name the doc_shield exception, matching the findings_raised row")

	assert.NotEmpty(t, reviewerCategories("sasha", []Finding{
		{Reviewers: []string{"sasha"}, Category: "security"},
	}), "the doc's 'populated since schema 2' claim rests on this fold producing values")

	assert.Contains(t, doc, "Opportunity-set scoping",
		"the TrustPriors filter list must name the opportunity-set link, or a reader cannot reproduce the denominator")

	// Each pass-through class the code implements must be named. A class the doc
	// omits reads to a maintainer as a class that gets judged.
	assert.True(t, len(opportunitySetRuns(
		[]Record{{RecordType: RecordTypeAggregate}},
		map[string]map[string]struct{}{},
	)) == 1, "aggregates pass through, which is what the doc claims")
	for _, class := range []string{"aggregates", "pre-schema-2", "non-discriminating"} {
		assert.Contains(t, doc, class,
			"the doc must name every record class the opportunity link passes through untouched")
	}
	// The empty-union class has THREE routes and the code cannot tell them apart.
	// Naming only one leaves an operator unable to explain the likeliest cause of
	// a whole panel going un-scoped.
	// Whitespace-normalised before matching. Pinning a prose fragment across a
	// hard line break plus its continuation indent makes an ordinary reflow fail
	// this test claiming the route is MISSING when it is merely rewrapped.
	flat := strings.Join(strings.Fields(doc), " ")
	assert.Contains(t, flat, "outside the closed vocabulary and was dropped at the write gate",
		"the doc must name the vocabulary-drop route to an empty union, not only the non-discriminating one")
	// The non-discriminating values are a closed set in remit.go; naming them in
	// the doc is how an operator reads a surprising score.
	for c := range nonDiscriminating {
		assert.Contains(t, doc, "`"+c+"`",
			"docs/scorecard.md must name every non-discriminating category by value")
	}

	// The floor interaction is the known limit an operator most needs; TD-025
	// tracks the re-measurement.
	assert.Contains(t, doc, "reduces the run count the `minRuns` floor sees",
		"the doc must state that opportunity scoping changes the quantity minRuns is applied to")
	// And the referent must exist. The doc sends a reader to the comment beside
	// DefaultTrustMinRuns; a pointer at a note that does not mention this link is
	// worse than no pointer, because it reads as already-explained.
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "scorecard", "trust.go"))
	require.NoError(t, err)
	minRunsBlock := string(src)
	cut := strings.Index(minRunsBlock, "const DefaultTrustMinRuns")
	require.Positive(t, cut, "DefaultTrustMinRuns declaration not found")
	assert.Contains(t, minRunsBlock[:cut], "opportunitySetRuns",
		"the doc points an operator at the comment beside DefaultTrustMinRuns; that comment must actually name the opportunity link")
}

// docTableRow returns the single markdown table row whose first cell contains
// key, so a pin defends the row it is about instead of the whole document.
func docTableRow(t *testing.T, doc, key string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Contains(line, key) {
			return line
		}
	}
	t.Fatalf("docs/scorecard.md has no table row for %s", key)
	return ""
}

// readDoc reads a file from docs/ relative to the repo root.
func readDoc(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", name))
	if err != nil {
		t.Fatalf("read docs/%s: %v", name, err)
	}
	return data
}

// TestDocs_PublicEnvelopeRaisedDenominator pins the PUBLIC-envelope reference
// against PublicRecord.RaisedDenominator (export.go): the field is deliberately
// NOT omitempty, so every exported reviewer row emits the key, and the docs must
// show it — in the reviewer field table, in the sample envelope, in the privacy
// allowlist, and in the benchmark submission sample (a benchmark row stamps
// RaisedDenominatorBenchmarkSuite, 100).
func TestDocs_PublicEnvelopeRaisedDenominator(t *testing.T) {
	sc, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "scorecard.md"))
	if err != nil {
		t.Fatalf("read docs/scorecard.md: %v", err)
	}
	doc := string(sc)
	for _, want := range []string{
		"| `raised_denominator` | int | always |", // reviewer field table row
		`"raised_denominator": 3`,                 // production sample envelope row
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md public-envelope reference is missing %s (PublicRecord.RaisedDenominator is not omitempty — every row emits it)", want)
		}
	}
	// The privacy allowlist bullet must name the key, or a reader concludes the
	// emitted key is off-allowlist.
	allowlistIdx := strings.Index(doc, "Preserved (allowlist):")
	if allowlistIdx < 0 {
		t.Fatalf("docs/scorecard.md privacy allowlist section not found")
	}
	strippedIdx := strings.Index(doc[allowlistIdx:], "Stripped / never exported:")
	if strippedIdx < 0 || !strings.Contains(doc[allowlistIdx:allowlistIdx+strippedIdx], "`raised_denominator`") {
		t.Errorf("privacy allowlist must list raised_denominator as preserved (a schema discriminator carrying no run content)")
	}

	bm, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "benchmark.md"))
	if err != nil {
		t.Fatalf("read docs/benchmark.md: %v", err)
	}
	if !strings.Contains(string(bm), `"raised_denominator": 100`) {
		t.Errorf("docs/benchmark.md sample submission is missing raised_denominator: 100 (benchmark rows stamp RaisedDenominatorBenchmarkSuite)")
	}
}

// TestDocs_ScorecardMdDocumentsDocShieldedColumn keeps the two rendered column
// lists in docs/scorecard.md honest about the conditional DOC-SHIELDED column.
//
// The column exists because Record.FindingsRaised stopped counting doc-shielded
// routings, so a reader of either table cannot otherwise tell a clean 100% from
// one with shielded findings behind it. A doc that omits the column teaches the
// old, now-ambiguous reading.
func TestDocs_ScorecardMdDocumentsDocShieldedColumn(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "scorecard.md"))
	if err != nil {
		t.Fatalf("read docs/scorecard.md: %v", err)
	}
	doc := string(data)

	assert.Contains(t, doc, "RAISED  CORROBORATED  SOLO  CORR%  COST  LATENCY",
		"the `atcr scorecard` base column list must stay documented verbatim")
	assert.Contains(t, doc, "RUNS  RAISED  CORROBORATED  CORR%  COST  COST/CORR  LATENCY",
		"the `atcr leaderboard` base column list must stay documented verbatim")
	assert.GreaterOrEqual(t, strings.Count(doc, "DOC-SHIELDED"), 2,
		"both column lists must document the conditional DOC-SHIELDED column, not just one")
}

// TestDocs_ScorecardMdDocumentsThePairSurface pins docs/scorecard.md against the
// pair fields' JSON tags and era constant.
//
// The era marker is the part worth a drift test rather than the field names.
// pair_era exists ONLY to make "measured, no pairs" distinguishable from
// "written before the field existed" — a reader who takes an absent pair_era as
// a measured zero reads the whole pre-existing store as a panel of lenses that
// never co-occur, which is the drop-candidate verdict applied to every pair. A
// doc that stops saying so is a doc that invites exactly that reading.
func TestDocs_ScorecardMdDocumentsThePairSurface(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	for _, want := range []string{
		"| `pair_signals` | array of object | conditional |",
		"| `pair_era` | int | conditional |",
		`"pair_era": 1,`,
		fmt.Sprintf("The measurement era for `pair_signals`, currently `%d`.", PairEraCurrent),
		// The attribution rule, not just the field's existence. An earlier
		// version of this row claimed disagreed "counts those the two split
		// on severity" full stop, which is false for any cluster of 3+ — and
		// this drift test was pinning the false claim in place.
		"**A split is only counted when the cluster held exactly two reviewers.**",
		"`gray_zone` disagreements are likewise not counted",
		// Both thresholds are provisional, and that warning has to reach a
		// READER of the doc rather than living only in Go comments.
		"The pair surface's two thresholds are provisional and unmeasured.",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md has drifted from the pair surface; missing:\n%s", want)
		}
	}

	// The fields are additive at v2 BY DECISION (C15), not by omission. If a
	// later change bumps SchemaVersion, this assertion fires and the doc's
	// "still at version 2" sentence has to be rewritten rather than left to
	// quietly contradict the constant.
	if SchemaVersion == 2 && !strings.Contains(doc, "**still at version `2`**") {
		t.Error("docs/scorecard.md no longer records that the pair fields are additive at v2")
	}
}

// TestDocs_ScorecardMdDocumentsTheWeightedCreditSurface guards docs/scorecard.md
// against drifting from the weighted-credit fields and their era constant.
//
// Two things here are worth a drift test rather than just the field names.
// credit_era, like pair_era, is the only thing in the bytes that separates a
// measured 0.0 from a record written before the field existed — and a reader who
// takes an absent marker as a measured zero drags the whole pre-existing store
// toward a zero score on upgrade. Separately, the doc has to keep saying that
// this number is NOT what reconcile consumes yet: the weighted rate sits on a
// different scale from corroboration_rate, and a reader who wires it into the
// unchanged exemption thresholds demotes most of the panel.
func TestDocs_ScorecardMdDocumentsTheWeightedCreditSurface(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	for _, want := range []string{
		"| `weighted_credit` | float | conditional |",
		"| `credit_era` | int | conditional |",
		`"credit_era": 1,`,
		fmt.Sprintf("The measurement era for `weighted_credit`, currently `%d`.", CreditEraCurrent),
		// The half-a-score caveat. Without it the field reads as a finished
		// quality number rather than the isolation half of one.
		"**This is only half the score.**",
		// The scale warning, which has to reach a reader of the doc rather than
		// living only in a Go comment.
		"The weighted-credit score is not wired into review yet",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md has drifted from the weighted-credit surface; missing:\n%s", want)
		}
	}
}

// TestDocs_ScorecardMdDocumentsTheRoutedCount guards the field that makes the
// weighted-credit ceiling computable at read time.
//
// It is worth its own pin because the doc has to keep the two "not in
// findings_raised" / "inside findings_raised" cases APART. Reading
// findings_routed as the doc-shield's twin, and subtracting both, under-bounds
// an honest record; reading neither leaves the bound loose by the routed count.
// A doc that blurs them invites either error.
func TestDocs_ScorecardMdDocumentsTheRoutedCount(t *testing.T) {
	doc := string(readDoc(t, "scorecard.md"))

	for _, want := range []string{
		"| `findings_routed` | int | conditional |",
		`"findings_routed": 1,`,
		"a doc-shielded finding is counted **instead of** being counted in `findings_raised`, a chargeable routed one is counted **inside** it",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/scorecard.md has drifted from the routed count; missing:\n%s", want)
		}
	}
}

// TestDocs_ScorecardMdDocumentsTheRaiserIsNeverDropped pins the opportunity
// gate's drop rule against the doc that publishes it.
//
// The rule changed at the 5.5 phase gate — the gate now drops ONLY a lens that
// raised nothing — and that change REPEALS a sentence this document used to
// state as the opportunity set's definition. A doc that keeps the old rule while
// the code runs the new one is worse than an undocumented one, because the old
// rule is the exact thing an operator would reason about when a score surprises
// them. So the doc must carry three things and all three are pinned here: the
// rule, the reason it changed, and what it cost.
func TestDocs_ScorecardMdDocumentsTheRaiserIsNeverDropped(t *testing.T) {
	flat := strings.Join(strings.Fields(string(readDoc(t, "scorecard.md"))), " ")

	assert.Contains(t, flat, "A record that RAISED findings is never dropped by this gate",
		"the doc must state the rule, or an operator cannot reproduce the denominator")
	assert.Contains(t, flat, "mislabelling profitable",
		"the doc must carry WHY the rule changed; the behaviour alone reads as an arbitrary choice")
	assert.Contains(t, flat, "holds for SILENT lenses only",
		"the doc must state what the change COST — the opportunity-set rule is narrowed, not merely clarified")
	assert.Contains(t, flat, "additionally annotated as *unlabelled*",
		"the doc must name the annotation the CASES column prints")

	// And the behaviour the doc describes must be the behaviour the code has.
	// Same reviewer, same run, same union; only the raised count differs.
	union := map[string]struct{}{"performance": {}}
	base := Record{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer,
		RunID: "r", Reviewer: "dax", Outcome: outcomeFindings,
	}

	unlabelledRaiser := base
	unlabelledRaiser.FindingsRaised = 3
	assert.Equal(t, dispUnscopeable, opportunityDisposition(unlabelledRaiser, union),
		"the doc says a raiser that attributed nothing is kept AND annotated")

	outOfRemitRaiser := base
	outOfRemitRaiser.FindingsRaised = 3
	outOfRemitRaiser.CategoriesRaised = []string{"performance"}
	assert.Equal(t, dispCounted, opportunityDisposition(outOfRemitRaiser, union),
		"the doc says a raiser under a real out-of-remit topic is simply counted")

	silent := base
	silent.Outcome = outcomeClean
	assert.Equal(t, dispOutOfRemit, opportunityDisposition(silent, union),
		"the doc says the narrowed rule still drops a lens that raised nothing")

	// The CLI wording the doc sends an operator to must be what the renderer
	// emits. formatScoreDetail lives in cli/, so the word is asserted here and
	// the rendering itself in cli/personas_test.go.
	assert.Contains(t, flat, "`CASES` column",
		"the doc names the column an operator reads this on")

	// THE LIMIT OF THE FIX MUST STAY DOCUMENTED. A gate round found that the
	// 1.00 escape is NOT closed on the production strict path — the phantom is
	// routed to the ambiguous stream, which carries a category and no count, so
	// it never reaches the gate with anything to charge. An earlier version of
	// this doc and of opportunityDisposition's comment both read as though the
	// escape itself had been closed. That overclaim is the thing most likely to
	// be restored by a well-meaning edit, so it is pinned.
	assert.Contains(t, flat, "What the change did NOT close",
		"the doc must state the limit of the fix, not only the fix")
	assert.Contains(t, flat, "never reaches this gate with a raised count at all",
		"the doc must name the ambiguous-stream mechanism that bypasses the charging rule")

	// And the code comment must agree with the doc, since a reader of one rarely
	// reads the other.
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "scorecard", "trust.go"))
	require.NoError(t, err)
	fn := string(src)
	cut := strings.Index(fn, "func opportunityDisposition(")
	require.Positive(t, cut, "opportunityDisposition not found")
	end := strings.Index(fn[cut:], "\n}\n")
	require.Positive(t, end, "opportunityDisposition has no closing brace")
	assert.Contains(t, fn[cut:cut+end], "WHAT THIS DID NOT CLOSE",
		"opportunityDisposition must carry the same limit the doc does; a reader of one rarely reads the other")
}
