package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift
// tests (see justification_record_boundary_test.go).
func TestBenchmarkDoc_FitWorkflowMatchesTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")

	t.Run("--replicates is the run flag the doc names", func(t *testing.T) {
		assert.Contains(t, doc, "### Replicates (`--replicates <n>`)")
		assert.Contains(t, doc, "--replicates 3 \\")
		code := readRepoFile(t, "../../cli/benchmark.go")
		assert.Contains(t, code, `cmd.Flags().Int("replicates", 1,`,
			"docs/benchmark.md documents `benchmark run --replicates`; a renamed flag must re-open it")
	})

	t.Run("benchmark fit --in is the command the doc names", func(t *testing.T) {
		assert.Contains(t, doc, "### `atcr benchmark fit --in <run-result.json>`")
		assert.Contains(t, doc, "atcr benchmark fit --in run-result.json")
		code := readRepoFile(t, "../../cli/benchmark_fit.go")
		assert.Contains(t, code, `Use:   "fit",`,
			"docs/benchmark.md documents `atcr benchmark fit`; a renamed subcommand must re-open it")
		assert.Contains(t, code, `cmd.Flags().String("in", "",`,
			"docs/benchmark.md documents `benchmark fit --in`; a renamed flag must re-open it")
	})

	t.Run("silent_chunks is the field the doc names", func(t *testing.T) {
		assert.Contains(t, doc, "\"chunk_count\", \"silent_chunks\", \"timed_out\"")
		assert.Contains(t, doc, "(`silent_chunks`\n  reaches `chunk_count`)")
		code := readRepoFile(t, "../../internal/fanout/status.go")
		assert.Contains(t, code, "SilentChunks int `json:\"silent_chunks,omitempty\"`",
			"docs/benchmark.md publishes the reviewer_fit field `silent_chunks`; a renamed JSON key must re-open it")
	})

	t.Run("fit-v1 is the suite the doc names, and it is not for submission", func(t *testing.T) {
		assert.Contains(t, doc, "--suite-path <atcr-repo>/benchmarks/fit-v1")
		assert.Contains(t, doc, "**`fit-v1` is not for submission.**")
		suite := readRepoFile(t, "../../benchmarks/fit-v1/suite.json")
		assert.Contains(t, suite, `"suite": "fit-v1"`,
			"docs/benchmark.md names the suite `fit-v1`; a renamed suite must re-open it")
		notice := readRepoFile(t, "../../benchmarks/fit-v1/NOTICE.md")
		assert.Contains(t, notice, "**Not for leaderboard submission.**",
			"the doc's not-for-submission rule restates the suite's NOTICE; if the NOTICE drops it, re-open the doc")
	})

	t.Run("fit-v1 needs review_strategy: chunked and a payload_byte_budget of at least 502,071", func(t *testing.T) {
		assert.Contains(t, doc, "**`fit-v1` needs `review_strategy: chunked`**")
		assert.Contains(t, doc, "**`payload_byte_budget` must be at least\n502,071**")
		assert.Contains(t, doc, "review_strategy: chunked\npayload_byte_budget: 524288\n")

		notice := readRepoFile(t, "../../benchmarks/fit-v1/NOTICE.md")
		assert.Contains(t, notice, "A `fit-v1` run needs `review_strategy: chunked`.",
			"the doc's chunked requirement restates the suite's NOTICE")
		assert.Contains(t, notice, "`payload_byte_budget` must be at least 502,071 bytes",
			"the doc's byte-budget floor restates the suite's NOTICE")
		assert.Contains(t, notice, "It is 502,071 bytes",
			"the floor is the case's own size; a regenerated case must re-open the doc's number")

		// Code anchors: the config keys and the strategy value the scratch config uses,
		// and the default the doc calls enough.
		project := readRepoFile(t, "../../internal/registry/project.go")
		assert.Contains(t, project, "`yaml:\"review_strategy,omitempty\"`")
		assert.Contains(t, project, "`yaml:\"payload_byte_budget,omitempty\"`")
		assert.Contains(t, project, "DefaultPayloadByteBudget int64 = 524288",
			"the doc says the default budget is 524,288 and enough; a smaller default must re-open it")
		chunker := readRepoFile(t, "../../internal/fanout/chunker.go")
		assert.Contains(t, chunker, `const reviewStrategyChunked = "chunked"`,
			"the doc's `review_strategy: chunked` must be the value the chunker acts on")
	})

	t.Run("the overlay roster must omit fallback:", func(t *testing.T) {
		assert.Contains(t, doc, "**The overlay must omit `fallback:`.**")
		assert.Contains(t, doc, "counted in `fallback_cases`")
		config := readRepoFile(t, "../../internal/registry/config.go")
		assert.Contains(t, config, "Fallback    string   `yaml:\"fallback,omitempty\"`",
			"the doc tells the operator to omit the agent key `fallback:`; a renamed key must re-open it")
		bench := readRepoFile(t, "../../internal/benchmark/benchmark.go")
		assert.Contains(t, bench, "FallbackCases int `json:\"fallback_cases,omitempty\"`",
			"the doc names `fallback_cases` as where a fallback-served case is counted")
		run := readRepoFile(t, "../../cli/benchmark_run.go")
		assert.Contains(t, run, "if a.FallbackUsed && a.FallbackModel != \"\" {\n\t\treturn a.FallbackModel",
			"the no-fallback rule exists because a failed-over case is scored under the backup's model; if that stops, re-open the rule")
	})
}
