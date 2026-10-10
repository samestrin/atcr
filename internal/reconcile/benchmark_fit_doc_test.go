package reconcile

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/benchmark.md tells an operator how to judge a persona + model pair before
// repointing a live agent: `benchmark run --replicates`, then `benchmark fit --in`,
// read through `silent_chunks`, on the `fit-v1` suite under `review_strategy:
// chunked`, from an overlay roster with no `fallback:`. Every one of those names is
// owned by code or by the suite's own NOTICE, not by the doc, so a rename on either
// side would leave the published workflow pointing at a flag, command, field or
// requirement that no longer exists.
//
// The guard is BIDIRECTIONAL, following benchmark_incomplete_routes_doc_test.go: the
// doc half asserts the name is published, the code half asserts the name is still
// what the code (or the suite NOTICE) uses. Renaming either side turns this red.
//
// Both halves match on whitespace-collapsed text, so a reflowed paragraph, a
// re-indented code block or a gofmt column realignment is not drift; only a changed
// name is.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift
// tests (see justification_record_boundary_test.go).
func TestBenchmarkDoc_FitWorkflowMatchesTheCode(t *testing.T) {
	doc := collapseWhitespace(readRepoFile(t, "../../docs/benchmark.md"))

	t.Run("--replicates is the run flag the doc names", func(t *testing.T) {
		assert.Contains(t, doc, "### Replicates (`--replicates <n>`)")
		assert.Contains(t, doc, "--replicates 3 \\")
		code := collapseWhitespace(funcBody(t, readRepoFile(t, "../../cli/benchmark.go"), "func newBenchmarkRunCmd("))
		assert.Contains(t, code, `cmd.Flags().Int("replicates", 1,`,
			"docs/benchmark.md documents `benchmark run --replicates`; a renamed flag must re-open it")
	})

	t.Run("--checkpoint and --output are the run flags the fit command block names", func(t *testing.T) {
		assert.Contains(t, doc, "--replicates 3 \\ --checkpoint fit.checkpoint.json --output run-result.json")
		code := collapseWhitespace(funcBody(t, readRepoFile(t, "../../cli/benchmark.go"), "func newBenchmarkRunCmd("))
		assert.Contains(t, code, `cmd.Flags().String("checkpoint", "",`,
			"docs/benchmark.md's fit run passes `benchmark run --checkpoint`; a renamed flag must re-open it")
		assert.Contains(t, code, `cmd.Flags().String("output", "",`,
			"docs/benchmark.md's fit run passes `benchmark run --output`; a renamed flag must re-open it")
	})

	t.Run("doctor --agents is the pre-flight the doc names", func(t *testing.T) {
		assert.Contains(t, doc, "**Check the agents answer at all:** `atcr doctor --agents <name>,<name>`.")
		code := collapseWhitespace(funcBody(t, readRepoFile(t, "../../cli/doctor.go"), "func newDoctorCmd("))
		assert.Contains(t, code, `Use: "doctor",`,
			"docs/benchmark.md's step 3 runs `atcr doctor`; a renamed command must re-open it")
		assert.Contains(t, code, `cmd.Flags().StringSlice("agents", nil,`,
			"docs/benchmark.md's step 3 passes a comma-separated `doctor --agents`; a renamed or re-typed flag must re-open it")
	})

	t.Run("benchmark fit --in is the command the doc names", func(t *testing.T) {
		assert.Contains(t, doc, "### `atcr benchmark fit --in <run-result.json>`")
		assert.Contains(t, doc, "atcr benchmark fit --in run-result.json")
		code := collapseWhitespace(readRepoFile(t, "../../cli/benchmark_fit.go"))
		assert.Contains(t, code, `Use: "fit",`,
			"docs/benchmark.md documents `atcr benchmark fit`; a renamed subcommand must re-open it")
		assert.Contains(t, code, `cmd.Flags().String("in", "",`,
			"docs/benchmark.md documents `benchmark fit --in`; a renamed flag must re-open it")
	})

	t.Run("silent_chunks is the field the doc names", func(t *testing.T) {
		assert.Contains(t, doc, "\"chunk_count\", \"silent_chunks\", \"timed_out\"")
		assert.Contains(t, doc, "(`silent_chunks` reaches `chunk_count`)")
		code := collapseWhitespace(readRepoFile(t, "../../internal/fanout/status.go"))
		assert.Contains(t, code, "SilentChunks int `json:\"silent_chunks,omitempty\"`",
			"docs/benchmark.md publishes the reviewer_fit field `silent_chunks`; a renamed JSON key must re-open it")
	})

	t.Run("fit-v1 is the suite the doc names, and it is not for submission", func(t *testing.T) {
		assert.Contains(t, doc, "--suite-path <atcr-repo>/benchmarks/fit-v1")
		assert.Contains(t, doc, "**`fit-v1` is not for submission.**")
		code := collapseWhitespace(funcBody(t, readRepoFile(t, "../../cli/benchmark.go"), "func newBenchmarkRunCmd("))
		assert.Contains(t, code, `cmd.Flags().String("suite-path", "",`,
			"docs/benchmark.md's fit run passes `benchmark run --suite-path`; a renamed flag must re-open it")
		suite := readRepoFile(t, "../../benchmarks/fit-v1/suite.json")
		assert.Contains(t, suite, `"suite": "fit-v1"`,
			"docs/benchmark.md names the suite `fit-v1`; a renamed suite must re-open it")
		notice := collapseWhitespace(readRepoFile(t, "../../benchmarks/fit-v1/NOTICE.md"))
		assert.Contains(t, notice, "**Not for leaderboard submission.**",
			"the doc's not-for-submission rule restates the suite's NOTICE; if the NOTICE drops it, re-open the doc")
	})

	t.Run("fit-v1 needs review_strategy: chunked and a payload_byte_budget of at least 502,071", func(t *testing.T) {
		assert.Contains(t, doc, "**`fit-v1` needs `review_strategy: chunked`**")
		assert.Contains(t, doc, "**`payload_byte_budget` must be at least 502,071**")
		assert.Contains(t, doc, "review_strategy: chunked payload_byte_budget: 524288 ```")

		notice := collapseWhitespace(readRepoFile(t, "../../benchmarks/fit-v1/NOTICE.md"))
		assert.Contains(t, notice, "A `fit-v1` run needs `review_strategy: chunked`.",
			"the doc's chunked requirement restates the suite's NOTICE")
		assert.Contains(t, notice, "`payload_byte_budget` must be at least 502,071 bytes",
			"the doc's byte-budget floor restates the suite's NOTICE")
		assert.Contains(t, notice, "It is 502,071 bytes",
			"the floor is the case's own size; a regenerated case must re-open the doc's number")

		// Code anchors: the config keys and the strategy value the scratch config uses,
		// and the default the doc calls enough.
		project := collapseWhitespace(readRepoFile(t, "../../internal/registry/project.go"))
		assert.Contains(t, project, "`yaml:\"review_strategy,omitempty\"`")
		assert.Contains(t, project, "`yaml:\"payload_byte_budget,omitempty\"`")
		assert.Contains(t, project, "DefaultPayloadByteBudget int64 = 524288",
			"the doc says the default budget is 524,288 and enough; a smaller default must re-open it")
		chunker := collapseWhitespace(readRepoFile(t, "../../internal/fanout/chunker.go"))
		assert.Contains(t, chunker, `const reviewStrategyChunked = "chunked"`,
			"the doc's `review_strategy: chunked` must be the value the chunker acts on")
	})

	t.Run("the overlay roster must omit fallback:", func(t *testing.T) {
		assert.Contains(t, doc, "**The overlay must omit `fallback:`.**")
		assert.Contains(t, doc, "counted in `fallback_cases`")
		config := collapseWhitespace(readRepoFile(t, "../../internal/registry/config.go"))
		assert.Contains(t, config, "Fallback string `yaml:\"fallback,omitempty\"`",
			"the doc tells the operator to omit the agent key `fallback:`; a renamed key must re-open it")
		bench := collapseWhitespace(readRepoFile(t, "../../internal/benchmark/benchmark.go"))
		assert.Contains(t, bench, "FallbackCases int `json:\"fallback_cases,omitempty\"`",
			"the doc names `fallback_cases` as where a fallback-served case is counted")
		// The no-fallback rule exists because a failed-over case is scored under the
		// backup's model; if reviewerModel stops reading the fallback, re-open the rule.
		model := funcBody(t, readRepoFile(t, "../../cli/benchmark_run.go"), "func reviewerModel(")
		for _, ident := range []string{"FallbackUsed", "FallbackModel"} {
			assert.Regexp(t, `\b`+ident+`\b`, model,
				"reviewerModel no longer reads %s; the doc's no-fallback rule must be re-opened", ident)
		}
		assert.Regexp(t, `return\s+a\.FallbackModel\b`, model,
			"reviewerModel no longer scores a failed-over case under the backup's model; re-open the doc's no-fallback rule")
	})

	t.Run("the live validation record leaks no host, path, run id or key", func(t *testing.T) {
		section := liveValidationSection(t, readRepoFile(t, "../../docs/benchmark.md"))
		assert.Empty(t, liveRecordLeaks(section),
			"the dated `#### Live validation` record must not carry an address, filesystem path, run id or key")
	})
}

// TestLiveRecordLeaks proves the no-leak checker above can fail: every bad input
// must be flagged, and the benign shapes the record does use (dates, short commit
// ids, commit ranges, replicate fractions, token counts) must not be.
func TestLiveRecordLeaks(t *testing.T) {
	bad := []struct{ name, in string }{
		{"IPv4 address", "served from 10.0.0.12 on the LAN"},
		{"IPv4 address with port", "base_url 192.168.1.40:8080"},
		{"host:port", "probed gauntlet.lan:11434 first"},
		{"localhost:port", "a proxy on localhost:4000"},
		{"URL", "see https://example.com/run"},
		{"absolute unix path", "the result is at /workspace/atcr/run-result.json"},
		{"absolute path in code span", "wrote `/tmp/fit/run.json`"},
		{"home path", "from ~/.atcr/registry.yaml"},
		{"windows path", `saved to C:\Users\op\run.json`},
		{"UUID run id", "run 0a00a849-2877-4ee1-b467-a4a528a84ee5 finished"},
		{"anthropic-style key", "key sk-ant-api03-AbCdEfGhIjKlMnOpQrSt"},
		{"github token", "ghp_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"},
		{"aws access key", "AKIAIOSFODNN7EXAMPLE"},
		{"bearer token", "Authorization: Bearer abc.def.ghi"},
		{"key assignment", "api_key=abcdef123456"},
		{"long opaque token", "token 9f8e7d6c5b4a39281706f5e4d3c2b1a0ffeeddccbbaa"},
	}
	for _, tc := range bad {
		t.Run("flags "+tc.name, func(t *testing.T) {
			assert.NotEmpty(t, liveRecordLeaks(tc.in), "checker missed a leak in %q", tc.in)
		})
	}

	good := []string{
		"#### Live validation, 2026-10-10 (atcr commit 23d0934)",
		"`4c588b6b..c08f657c` (96 files); `fit-v1` is `4c588b6b..8576a42` without Markdown",
		"| ronin | nemotron-3-super-120b | unfit (truncated 1/3) | 0, 3, 0 | 48376, 53539, 31906 | matches (unfit) |",
		"pace + gpt-oss-120b's 3–9 findings per replicate sit inside the 3–7 band",
		"(`fit` and `fit (warning)` both count as fit)",
	}
	for _, in := range good {
		assert.Empty(t, liveRecordLeaks(in), "checker flagged a benign line %q", in)
	}
}

// collapseWhitespace folds every run of whitespace to one space, so a match does not
// depend on line wraps, indentation or gofmt column alignment.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// liveValidationSection returns the `#### Live validation` record: from its heading
// (whose date and commit change with each run) to the next heading or rule.
func liveValidationSection(t *testing.T, doc string) string {
	t.Helper()
	start := strings.Index(doc, "\n#### Live validation")
	require.GreaterOrEqual(t, start, 0, "docs/benchmark.md has no `#### Live validation` record")
	rest := doc[start+1:]
	if end := regexp.MustCompile(`\n(#{1,4} |---\n)`).FindStringIndex(rest); end != nil {
		rest = rest[:end[0]]
	}
	return rest
}

// liveRecordLeakPatterns are the shapes the plan's no-leak rule forbids in a dated
// record: an address, a filesystem path, a run id, or a credential.
var liveRecordLeakPatterns = []struct {
	what string
	re   *regexp.Regexp
}{
	{"IPv4 address", regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)},
	{"host:port", regexp.MustCompile(`\b(localhost|[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+):\d{1,5}\b`)},
	{"URL", regexp.MustCompile(`\b[a-z][a-z0-9+.-]*://`)},
	{"absolute path", regexp.MustCompile("(^|[\\s(`\"'=])(/[\\w.-]+/|~/|[A-Za-z]:\\\\)")},
	{"UUID", regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)},
	{"prefixed key", regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|xox[abprs]-[A-Za-z0-9-]{10,})`)},
	{"bearer token", regexp.MustCompile(`(?i)\bbearer\s+\S+`)},
	{"key assignment", regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password)\s*[:=]\s*\S+`)},
	{"long opaque token", regexp.MustCompile(`\b[A-Za-z0-9_-]{32,}\b`)},
}

// liveRecordLeaks returns one description per forbidden shape found in s.
func liveRecordLeaks(s string) []string {
	var leaks []string
	for _, p := range liveRecordLeakPatterns {
		for _, m := range p.re.FindAllString(s, -1) {
			leaks = append(leaks, p.what+": "+strings.TrimSpace(m))
		}
	}
	return leaks
}
