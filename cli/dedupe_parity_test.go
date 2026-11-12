package cli

// dedupe_parity_test.go is the parity guard between `atcr dedupe` and the tool
// it replaces, `llm-support td-dedupe` (epic 110 T4). It runs the command over
// the fixture streams from llm-tools' own tests and compares the decoded JSON
// with the output captured from the real `td-dedupe`, so a change in atcr's
// clustering, merging, counting or wire shape shows up as a diff instead of
// reaching `reconcile-code-review` silently.
//
// The goldens are td-dedupe's output verbatim. The only values the test lets
// differ are the rows of the "Allowed deviations" table in
// testdata/dedupe/DIFFERENCES.md, which the test READS rather than duplicating:
// a new deviation cannot land without a documented row, and a documented row
// that no longer matches a golden fails as stale — checked row by row, so a
// stale row cannot hide behind a still-applying sibling in the same case. That
// is deliberate — the
// issue's contract is "DIFFERENCES.md is the only allowed deviation", and a
// second, hand-kept list in Go would be free to drift from it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dedupeParityDir        = "testdata/dedupe"
	dedupeParityStreamsDir = "testdata/dedupe/streams"
	dedupeParityDiffDoc    = "testdata/dedupe/DIFFERENCES.md"
)

// dedupeParityStream is one input stream: its file under streams/ and the
// source tag passed with --source-tags.
type dedupeParityStream struct {
	file string
	tag  string
}

// dedupeParityCase is one captured run. Each case names the llm-tools test that
// carries the same fixture streams.
type dedupeParityCase struct {
	name      string
	streams   []dedupeParityStream
	tolerance int
	untrusted []string
}

func dedupeParityCases() []dedupeParityCase {
	return []dedupeParityCase{
		// td_dedupe_test.go: dedupeStreamA + dedupeStreamB, the clusters-and-
		// aggregates case (also the singletons-are-MEDIUM case, which asserts
		// the same golden).
		{name: "two-reviewers-merge", streams: []dedupeParityStream{
			{"two-reviewers-claude.txt", "claude"},
			{"two-reviewers-multi-agent.txt", "multi-agent"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_DuplicateReviewerNotDoubleCounted.
		{name: "duplicate-reviewer", streams: []dedupeParityStream{
			{"duplicate-reviewer-claude.txt", "claude"},
			{"duplicate-reviewer-claude2.txt", "claude2"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_SyntheszeReviewerFromTag.
		{name: "reviewer-from-tag", streams: []dedupeParityStream{
			{"reviewer-from-tag.txt", "external"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_FarApartSameFileNotMerged.
		{name: "far-apart", streams: []dedupeParityStream{
			{"far-apart.txt", "c"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_UntrustedLow.
		{name: "untrusted-source", streams: []dedupeParityStream{
			{"untrusted-source.txt", "sketchy"},
		}, tolerance: 3, untrusted: []string{"sketchy"}},

		// td_dedupe_test.go: TestDedupeTD_NoLineRows.
		{name: "no-line-rows", streams: []dedupeParityStream{
			{"no-line-rows.txt", "c"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_MalformedRowSurfaced.
		{name: "malformed-row", streams: []dedupeParityStream{
			{"malformed-row.txt", "c"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_TenColLegacyClaude.
		{name: "ten-column-legacy", streams: []dedupeParityStream{
			{"ten-column-legacy.txt", "claude"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_EmptyStreams.
		{name: "empty-streams", streams: []dedupeParityStream{
			{"empty.txt", "a"},
			{"comment-only.txt", "b"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_CategoryVoteNotSplitByCasing. Every
		// spelling here canonicalizes to the same word atcr votes with, so this
		// case needs no deviation row.
		{name: "category-vote", streams: []dedupeParityStream{
			{"category-vote-security.txt", "a"},
			{"category-vote-performance-upper.txt", "b"},
			{"category-vote-performance-lower.txt", "c"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_MembersCarryOwnFixAndCitation.
		{name: "members-carry-own", streams: []dedupeParityStream{
			{"members-claude.txt", "claude"},
			{"members-multi-agent.txt", "multi-agent"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_MembersAcrossMixedStreamWidths. The
		// fixture's 1-1 category tie is a documented deviation (tie-break order).
		{name: "members-across-widths", streams: []dedupeParityStream{
			{"ten-column-legacy.txt", "claude"},
			{"members-eight-column.txt", "multi-agent"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_MemberOrderIsLineSorted.
		{name: "member-order", streams: []dedupeParityStream{
			{"member-order-later.txt", "a"},
			{"member-order-earlier.txt", "b"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_EmptyMemberFieldNotBackFilled.
		{name: "empty-member-field", streams: []dedupeParityStream{
			{"empty-member-a.txt", "a"},
			{"empty-member-b.txt", "b"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_SingletonHasNoMembers.
		{name: "singleton-solo", streams: []dedupeParityStream{
			{"singleton-solo.txt", "a"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_PooledTextCollapsesRestatementsKeepsDistinct,
		// the three sub-cases in one run: identical, contained, distinct remedies.
		{name: "pooled-text", streams: []dedupeParityStream{
			{"pooled-identical-claude.txt", "claude"},
			{"pooled-identical-multi-agent.txt", "multi-agent"},
			{"pooled-contained-claude.txt", "claude"},
			{"pooled-contained-multi-agent.txt", "multi-agent"},
			{"pooled-distinct-claude.txt", "claude"},
			{"pooled-distinct-multi-agent.txt", "multi-agent"},
		}, tolerance: 3},

		// td_dedupe_header_test.go: canonicalV1.
		{name: "canonical-v1", streams: []dedupeParityStream{
			{"canonical-v1.txt", "claude"},
		}, tolerance: 3},

		// td_dedupe_header_test.go: postBlocking (a declared BLOCKING column).
		{name: "format-blocking-column", streams: []dedupeParityStream{
			{"format-blocking-column.txt", "post"},
		}, tolerance: 3},

		// td_dedupe_header_test.go: epicEvidenceSource (a declared SOURCE column).
		{name: "format-evidence-source", streams: []dedupeParityStream{
			{"format-evidence-source.txt", "epic"},
		}, tolerance: 3},

		// td_dedupe_header_test.go: legacyNineCol, the layout that exists on disk.
		{name: "format-nine-column", streams: []dedupeParityStream{
			{"format-nine-column.txt", "claude"},
		}, tolerance: 3},

		// td_dedupe_header_test.go: TestALeadingBlankLineDoesNotHideTheFormatComment.
		{name: "leading-blank-line", streams: []dedupeParityStream{
			{"leading-blank-line.txt", "claude"},
		}, tolerance: 3},

		// td_dedupe_header_test.go: TestAHeaderlessStreamStillParsesAndWarns.
		{name: "headerless-stream", streams: []dedupeParityStream{
			{"headerless-stream.txt", "claude"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestDedupeTD_MemberCategoryNormalized. Both raw
		// spellings are outside atcr's canonical vocabulary (PERF, the `_`-joined
		// ERROR_HANDLING), which is a documented deviation.
		{name: "member-category-normalized", streams: []dedupeParityStream{
			{"member-category-perf.txt", "a"},
			{"member-category-underscore.txt", "b"},
		}, tolerance: 3},

		// td_dedupe_test.go: TestNormalizeCategory's alias and unknown-word
		// spellings, one cluster each so each output category is attributable.
		{name: "category-aliases", streams: []dedupeParityStream{
			{"diff-alias-perf.txt", "claude"},
			{"diff-alias-doc.txt", "claude"},
			{"diff-alias-tests.txt", "claude"},
			{"diff-unknown-word.txt", "claude"},
		}, tolerance: 3},

		// The exact reserved control token passes through unchanged in both
		// tools; this case is the no-deviation control for the out-of-scope rows.
		{name: "category-out-of-scope-exact", streams: []dedupeParityStream{
			{"diff-out-of-scope-exact.txt", "claude"},
		}, tolerance: 3},

		// td_dedupe_test.go's own note that casing variants must not become the
		// control token: atcr treats Out-Of-Scope as an unknown word (deviation).
		{name: "category-out-of-scope-cased", streams: []dedupeParityStream{
			{"diff-out-of-scope-cased.txt", "claude"},
		}, tolerance: 3},

		// Written for this test: the panel-wide-instruction confidence discount.
		// No llm-tools fixture is in that class; see DIFFERENCES.md.
		{name: "panel-wide-confidence", streams: []dedupeParityStream{
			{"panel-wide-invariant-claude.txt", "claude"},
			{"panel-wide-invariant-multi-agent.txt", "multi-agent"},
		}, tolerance: 3},
	}
}

// dedupeDeviation is one allowed difference, read from DIFFERENCES.md.
type dedupeDeviation struct {
	index int    // position within its case's rows, the stale-row guard's key
	field string // category, member category, confidence or source
	from  string // the td-dedupe value in the golden
	to    string // the atcr dedupe value
}

// staleDedupeDeviations returns the rows that applyDedupeDeviations did not
// use. A row whose "from" value appears nowhere in its case's golden is
// documentation that has outlived its deviation.
func staleDedupeDeviations(rows []dedupeDeviation, applied map[int]bool) []dedupeDeviation {
	var stale []dedupeDeviation
	for _, d := range rows {
		if !applied[d.index] {
			stale = append(stale, d)
		}
	}
	return stale
}

// TestDedupeParityWithTdDedupe is the parity claim itself.
func TestDedupeParityWithTdDedupe(t *testing.T) {
	deviations := readDedupeDeviations(t)
	cases := dedupeParityCases()

	ran := map[string]bool{}
	// usedDeviation is keyed by case and then by the row's index within that
	// case, so a case with several rows cannot hide one stale row behind its
	// siblings.
	usedDeviation := map[string]map[int]bool{}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran[tc.name] = true
			args := dedupeParityArgs(tc)
			code, stdout, stderr := execCmdSplit(t, args...)
			require.Equal(t, 0, code, "atcr dedupe failed: %s", stderr)

			var actual map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &actual), "atcr dedupe must print JSON: %s", stdout)

			raw, err := os.ReadFile(filepath.Join(dedupeParityDir, tc.name+".golden.json"))
			require.NoError(t, err, "every parity case needs a captured td-dedupe golden")
			var golden map[string]any
			require.NoError(t, json.Unmarshal(raw, &golden), "%s.golden.json must be valid JSON", tc.name)

			applied := applyDedupeDeviations(golden, deviations[tc.name])
			if usedDeviation[tc.name] == nil {
				usedDeviation[tc.name] = map[int]bool{}
			}
			for i := range applied {
				usedDeviation[tc.name][i] = true
			}

			assert.Equal(t, golden, actual,
				"%s: `atcr dedupe` must equal the captured `td-dedupe` output except for the rows DIFFERENCES.md allows", tc.name)
		})
	}

	// A row for a case that no longer exists, or whose value no golden carries,
	// is documentation that has outlived its deviation. Only cases this run
	// actually exercised are checked, so -run filtering does not fail the guard.
	for name := range deviations {
		if !ran[name] {
			continue
		}
		for _, d := range staleDedupeDeviations(deviations[name], usedDeviation[name]) {
			assert.Failf(t, "stale deviation row",
				"DIFFERENCES.md: case %q field %q row %q->%q matches no value in its golden; drop the row or restore the fixture",
				name, d.field, d.from, d.to)
		}
	}

	// And the reverse: no golden left unverified.
	matches, err := filepath.Glob(filepath.Join(dedupeParityDir, "*.golden.json"))
	require.NoError(t, err)
	known := map[string]bool{}
	for _, tc := range cases {
		known[tc.name] = true
	}
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), ".golden.json")
		assert.True(t, known[name], "golden %q has no parity case; add it to dedupeParityCases or delete the file", name)
	}
}

// TestDedupeParityStaleRowFailsDespiteSiblings is the reverse-direction guard:
// a row whose value no longer appears in its golden must be reported as stale
// even when other rows for the same case still apply. Without per-row tracking,
// a case with three rows would let one retired row ride along on its siblings.
func TestDedupeParityStaleRowFailsDespiteSiblings(t *testing.T) {
	rows := readDedupeDeviations(t)["member-category-normalized"]
	require.Len(t, rows, 3, "this guard pins a case with sibling rows plus one stale row")

	load := func() map[string]any {
		raw, err := os.ReadFile(filepath.Join(dedupeParityDir, "member-category-normalized.golden.json"))
		require.NoError(t, err)
		var golden map[string]any
		require.NoError(t, json.Unmarshal(raw, &golden))
		return golden
	}

	// Sanity: every row applies to the pristine golden, so nothing is stale yet.
	applied := applyDedupeDeviations(load(), rows)
	assert.Empty(t, staleDedupeDeviations(rows, applied), "the committed golden must still satisfy every row")

	// Mutate only the golden's `error-handling` member category to `other`, the
	// same mutation the reviewer used to show the old guard was blind: that row's
	// `from` no longer exists, while the two `performance` rows still apply.
	golden := load()
	merged, ok := golden["merged"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, merged)
	row, ok := merged[0].(map[string]any)
	require.True(t, ok)
	members, ok := row["members"].([]any)
	require.True(t, ok)
	mutated := false
	for _, m := range members {
		member, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if member["category"] == "error-handling" {
			member["category"] = "other"
			mutated = true
		}
	}
	require.True(t, mutated, "the fixture must still carry the `error-handling` member category")

	applied = applyDedupeDeviations(golden, rows)
	stale := staleDedupeDeviations(rows, applied)
	require.Len(t, stale, 1, "only the row whose value disappeared may be stale")
	assert.Equal(t, "member category", stale[0].field)
	assert.Equal(t, "error-handling", stale[0].from)
}

// TestDedupeParityPanelWideDiscountIsDocumented proves the confidence deviation
// the parity test allows is the panel-wide discount and not an accident: the two
// reviewers agree on the file and the invariant category, and only the discount
// separates HIGH from MEDIUM.
func TestDedupeParityPanelWideDiscountIsDocumented(t *testing.T) {
	deviations := readDedupeDeviations(t)["panel-wide-confidence"]
	found := false
	for _, d := range deviations {
		if d.field == "confidence" && d.from == "HIGH" && d.to == "MEDIUM" {
			found = true
		}
	}
	require.True(t, found, "the panel-wide case must keep its documented HIGH->MEDIUM confidence deviation")

	var golden struct {
		Merged []struct {
			Confidence string `json:"confidence"`
			Reviewers  string `json:"reviewers"`
			Category   string `json:"category"`
		} `json:"merged"`
	}
	raw, err := os.ReadFile(filepath.Join(dedupeParityDir, "panel-wide-confidence.golden.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &golden))
	require.Len(t, golden.Merged, 1)
	assert.Equal(t, "HIGH", golden.Merged[0].Confidence)
	assert.Equal(t, "invariant", golden.Merged[0].Category)
	assert.Len(t, strings.Split(golden.Merged[0].Reviewers, ","), 2, "the discount needs 2+ reviewers")

	var actual struct {
		Merged []struct {
			Confidence string `json:"confidence"`
		} `json:"merged"`
	}
	tc := dedupeParityCaseByName(t, "panel-wide-confidence")
	code, stdout, stderr := execCmdSplit(t, dedupeParityArgs(tc)...)
	require.Equal(t, 0, code, stderr)
	require.NoError(t, json.Unmarshal([]byte(stdout), &actual))
	require.Len(t, actual.Merged, 1)
	assert.Equal(t, "MEDIUM", actual.Merged[0].Confidence,
		"reconcile.Merge's panel-wide discount is the one thing that lowers this cluster to MEDIUM")
}

// dedupeParityArgs builds the command line for one case, always passing
// --source-tags so the tag is the fixture's, not a temp directory's name.
func dedupeParityArgs(tc dedupeParityCase) []string {
	tags := make([]string, len(tc.streams))
	for i, s := range tc.streams {
		tags[i] = s.tag
	}
	args := []string{
		"dedupe",
		"--streams", dedupeParityPaths(tc),
		"--source-tags", strings.Join(tags, ","),
		"--tolerance", strconv.Itoa(tc.tolerance),
	}
	if len(tc.untrusted) > 0 {
		args = append(args, "--untrusted", strings.Join(tc.untrusted, ","))
	}
	return args
}

func dedupeParityPaths(tc dedupeParityCase) string {
	paths := make([]string, len(tc.streams))
	for i, s := range tc.streams {
		paths[i] = filepath.Join(dedupeParityStreamsDir, s.file)
	}
	return strings.Join(paths, ",")
}

// applyDedupeDeviations rewrites the golden's category/source/confidence values
// through the documented deviations and returns the indexes of the rows it
// used, so the caller can detect a stale row at the row level. Fields without a
// deviation are left alone, so the comparison stays exact.
func applyDedupeDeviations(golden map[string]any, deviations []dedupeDeviation) map[int]bool {
	applied := map[int]bool{}
	rows, ok := golden["merged"].([]any)
	if !ok {
		return applied
	}
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		applyDedupeDeviation(row, deviations, "category", applied)
		applyDedupeDeviation(row, deviations, "confidence", applied)
		applyDedupeDeviation(row, deviations, "source", applied)
		if members, ok := row["members"].([]any); ok {
			for _, mr := range members {
				if member, ok := mr.(map[string]any); ok {
					// A member carries a category but never a confidence or source.
					applyDedupeDeviation(member, deviations, "member category", applied)
				}
			}
		}
	}
	return applied
}

func applyDedupeDeviation(value map[string]any, deviations []dedupeDeviation, field string, applied map[int]bool) {
	// "member category" is the JSON field "category" on a member row; the field
	// name in DIFFERENCES.md carries the row it applies to.
	key := field
	if field == "member category" {
		key = "category"
	}
	got, ok := value[key].(string)
	if !ok {
		return
	}
	for _, d := range deviations {
		if d.field == field && d.from == got {
			value[key] = d.to
			applied[d.index] = true
			return
		}
	}
}

// readDedupeDeviations parses the "Allowed deviations" table in
// DIFFERENCES.md, keyed by case name. The table is the single source of truth:
// the test does not repeat its values in Go.
func readDedupeDeviations(t *testing.T) map[string][]dedupeDeviation {
	t.Helper()
	raw, err := os.ReadFile(dedupeParityDiffDoc)
	require.NoError(t, err, "the parity differences file must be readable")

	cases := dedupeParityCases()
	known := map[string]bool{}
	for _, c := range cases {
		known[c.name] = true
	}

	out := map[string][]dedupeDeviation{}
	inTable := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			// Any non-table line ends the table; the file has one deviations table.
			inTable = false
			continue
		}
		cells := dedupeTableCells(line)
		switch {
		case len(cells) != 5:
			continue
		case cells[0] == "Case": // header row
			inTable = true
			continue
		case strings.Trim(cells[0], "-: ") == "": // header separator
			continue
		case !inTable:
			continue
		}
		name, field, from, to := cells[0], cells[1], cells[2], cells[3]
		require.Truef(t, known[name], "%s: row %q names a case that does not exist", dedupeParityDiffDoc, name)
		require.Containsf(t, []string{"category", "member category", "confidence", "source"}, field,
			"%s: row %q names field %q; only category, member category, confidence and source may differ", dedupeParityDiffDoc, name, field)
		// cells[4] is the human "Why" column; it is prose, not test input.
		out[name] = append(out[name], dedupeDeviation{index: len(out[name]), field: field, from: from, to: to})
	}
	require.NotEmpty(t, out, "%s must list the allowed deviations", dedupeParityDiffDoc)
	return out
}

// dedupeTableCells splits one markdown table row and strips the backticks the
// doc wraps code values in.
func dedupeTableCells(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.Trim(strings.TrimSpace(p), "`")
	}
	return cells
}

// dedupeParityCaseByName returns the one case with that name.
func dedupeParityCaseByName(t *testing.T, name string) dedupeParityCase {
	t.Helper()
	for _, tc := range dedupeParityCases() {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("no parity case named %q", name)
	return dedupeParityCase{}
}
