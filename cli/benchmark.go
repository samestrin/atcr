package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/spf13/cobra"
)

// newBenchmarkCmd builds `atcr benchmark`: the standard-suite tooling for the
// public Model-Eval Leaderboard (Epic 10.0 / 10.2). `verify` validates a suite
// manifest and prints its reproducibility hash; `run` executes a suite through the
// review pipeline and writes a scored run-result; `export` wraps a run-result in
// the suite-tagged public submission envelope. The curated standard-v1 suite
// content is bundled at benchmarks/standard-v1/ in this repo.
func newBenchmarkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Standard benchmark-suite tooling for the public leaderboard",
		Long: "Tooling for the standard benchmark suite that feeds the public Model-Eval\n" +
			"Leaderboard. `verify` validates a suite manifest and prints its\n" +
			"reproducibility hash; `run` executes the suite through the review pipeline\n" +
			"and writes a scored run-result; `export` produces a suite-tagged public\n" +
			"submission record (distinct from `leaderboard --export`, so suite runs are\n" +
			"distinguishable from production runs on the public board).",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newBenchmarkVerifyCmd(), newBenchmarkRunCmd(), newBenchmarkExportCmd())
	return cmd
}

// newBenchmarkVerifyCmd builds `atcr benchmark verify --suite-path <dir>`: load
// and validate the suite manifest, confirm every case diff exists, and print the
// deterministic reproducibility hash. Read-only.
func newBenchmarkVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Validate a benchmark suite manifest and print its reproducibility hash",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runBenchmarkVerify,
	}
	cmd.Flags().String("suite-path", "", "path to the suite directory (containing suite.json)")
	_ = cmd.MarkFlagRequired("suite-path")
	return cmd
}

func runBenchmarkVerify(cmd *cobra.Command, _ []string) error {
	// Cobra GetString error is unreachable: flag registered above, MarkFlagRequired
	// enforces presence before RunE executes. Project-wide convention (27 sites).
	suitePath, _ := cmd.Flags().GetString("suite-path")

	m, err := benchmark.Load(suitePath)
	if err != nil {
		return err
	}
	hash, err := benchmark.ReproHashManifest(m, suitePath)
	if err != nil {
		return err
	}
	noun := "cases"
	if len(m.Cases) == 1 {
		noun = "case"
	}
	_, werr := fmt.Fprintf(cmd.OutOrStdout(),
		"suite %q version %s: %d %s, valid\nreproducibility hash: %s\n",
		m.Suite, m.SuiteVersion, len(m.Cases), noun, hash)
	return werr
}

// newBenchmarkRunCmd builds `atcr benchmark run --suite-path <dir> [--out <file>]`:
// load + validate the suite, execute each case's diff through the review pipeline
// (the diff-file ingestion path), score the findings against each case's expected
// categories, and write the suite-tagged run-result that `benchmark export`
// consumes. The run-result's GeneratedAt is stamped from the wall clock here; the
// scoring is deterministic given the same suite + transcript.
func newBenchmarkRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a benchmark suite through the review pipeline and write a scored run-result",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runBenchmarkRun,
	}
	cmd.Flags().String("suite-path", "", "path to the suite directory (containing suite.json)")
	cmd.Flags().String("output", "", "write the run-result JSON to this file instead of stdout (atomically replaces the target; a symlink at the path is replaced, not followed)")
	cmd.Flags().String("out", "", "deprecated alias for --output")
	// Hidden rather than MarkDeprecated — see addDebtStoreFlag for why (the
	// parse-time warning pollutes merged-stream output consumers).
	_ = cmd.Flags().MarkHidden("out")
	cmd.Flags().String("checkpoint", "", "opt-in: path to a run checkpoint file (atomically replaces the target; a symlink at the path is replaced, not followed). Each scored case is durably recorded here before the next begins; re-running the same suite resumes from the first unscored case instead of restarting (and re-paying for) the whole run. The path must not be shared across concurrent benchmark run invocations. Empty = no checkpointing (default).")
	_ = cmd.MarkFlagRequired("suite-path")
	return cmd
}

func runBenchmarkRun(cmd *cobra.Command, _ []string) error {
	// Cobra GetString errors are unreachable: all flags are registered above
	// ("suite-path" is MarkFlagRequired). Project-wide convention.
	suitePath, _ := cmd.Flags().GetString("suite-path")
	// --output is canonical; --out is the deprecated alias (honored when --output
	// is unset).
	out, _ := cmd.Flags().GetString("output")
	if !cmd.Flags().Changed("output") && cmd.Flags().Changed("out") {
		out, _ = cmd.Flags().GetString("out")
	}
	checkpoint, _ := cmd.Flags().GetString("checkpoint")

	// Discover config the same way `atcr review` does (registry + project config
	// rooted at the cwd), so the benchmark roster is the project's reviewers.
	cfg, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
	if err != nil {
		return err
	}

	// Audit identity (Epic 35.0): a benchmark drives many models over many
	// cases, so without a stage those records are unattributable in a stream
	// shared with real review work.
	benchCtx := hookobs.WithCall(cmd.Context(), hookobs.Call{Stage: "benchmark"})
	rr, err := executeBenchmarkRun(benchCtx, cfg, newCompleter(benchCtx), suitePath, time.Now().UTC(), checkpoint)
	if err != nil {
		return err
	}

	warnVocabularyDiagnostics(cmd.ErrOrStderr(), rr)

	data, err := json.MarshalIndent(rr, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding run-result: %w", err)
	}
	if out == "" {
		_, werr := cmd.OutOrStdout().Write(append(data, '\n'))
		return werr
	}
	// writeExportFile (leaderboard.go) atomically writes to path, creating parents.
	return writeExportFile(out, data)
}

// newBenchmarkExportCmd builds `atcr benchmark export --in <run-result.json>`:
// read a suite run-result and emit the suite-tagged public submission envelope.
// The run-result is produced by `atcr benchmark run`; export reads it rather than
// the local scorecard, so a production run can never be passed off as a suite
// submission.
func newBenchmarkExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Emit a suite-tagged public submission record from a benchmark run-result",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runBenchmarkExport,
	}
	cmd.Flags().String("in", "", "path to a benchmark run-result JSON file (produced by atcr benchmark run)")
	cmd.Flags().String("output", "", "write the submission JSON to this file instead of stdout (atomically replaces the target; a symlink at the path is replaced, not followed)")
	cmd.Flags().String("suite-path", "", "path to the suite directory (containing suite.json) the run-result was produced from. Optional: when given, the run-result's suite_case_ids must equal the manifest's case list, which anchors the coverage gate's denominator to the suite instead of to the file being checked. Without it the gate can only prove the file is internally consistent — a run-result that truncated its own case list passes.")
	cmd.Flags().Bool("allow-partial-coverage", false, "publish even when a reviewer row was scored over less than the full suite. Off by default: rows measured over different subsets of the suite are not comparable, and a mid-run model failover makes partial coverage a normal outcome rather than an exotic one. When set, the shortfall is recorded in the run-result only — the submission does not carry it, so consumers cannot distinguish these rows from fully-covered ones.")
	_ = cmd.MarkFlagRequired("in")
	return cmd
}

func runBenchmarkExport(cmd *cobra.Command, _ []string) error {
	// Cobra GetString errors are unreachable: both flags are registered above
	// ("in" is MarkFlagRequired), so GetString returns the flag value or its
	// default, never an error. Project-wide convention (27 sites).
	in, _ := cmd.Flags().GetString("in")
	output, _ := cmd.Flags().GetString("output")
	allowPartial, _ := cmd.Flags().GetBool("allow-partial-coverage")
	suitePath, _ := cmd.Flags().GetString("suite-path")

	data, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("reading run-result %s: %w", in, err)
	}
	var rr benchmark.RunResult
	if err := json.Unmarshal(data, &rr); err != nil {
		return fmt.Errorf("parsing run-result %s: %w", in, err)
	}
	if strings.TrimSpace(rr.Suite) == "" || strings.TrimSpace(rr.SuiteVersion) == "" {
		return fmt.Errorf("run-result %s is missing suite/suite_version", in)
	}
	if len(rr.Reviewers) == 0 {
		return fmt.Errorf("run-result %s has no reviewers", in)
	}
	// An unidentifiable reviewer row on a public leaderboard is worse than a
	// rejected file: same TrimSpace rule as the suite identity above.
	for i, rev := range rr.Reviewers {
		if strings.TrimSpace(rev.Model) == "" || strings.TrimSpace(rev.Persona) == "" {
			return fmt.Errorf("run-result %s has reviewer %d with empty model/persona", in, i)
		}
	}
	// A run-result may be hand-supplied, so the diagnostic is untrusted input here.
	// out_of_vocabulary_rate is a SHARE of findings: a value outside [0,1] (or NaN)
	// is a corrupt file rather than a pessimistic reading, and must not be carried
	// forward as a measurement. nil stays legal — it means unmeasured.
	if rr.OutOfVocabularyRate != nil {
		if v := *rr.OutOfVocabularyRate; math.IsNaN(v) || v < 0 || v > 1 {
			return fmt.Errorf("run-result %s has out_of_vocabulary_rate %v outside [0,1]", in, v)
		}
	}

	// Anchor before the gate, not after: checkCoverage's every diagnostic is phrased
	// against rr.SuiteCaseIDs, so a truncated denominator would otherwise produce a
	// clean bill of health that the anchor then contradicts.
	if suitePath != "" {
		if err := anchorSuiteDenominator(rr, suitePath, in); err != nil {
			return err
		}
	}
	if err := checkCoverage(cmd.ErrOrStderr(), rr, in, allowPartial); err != nil {
		return err
	}

	generatedAt, err := time.Parse(time.RFC3339, rr.GeneratedAt)
	if err != nil {
		return fmt.Errorf("parsing generated_at %q: %w", rr.GeneratedAt, err)
	}
	sub := benchmark.BuildSubmission(rr, generatedAt)
	out, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding submission: %w", err)
	}
	if output == "" {
		_, werr := cmd.OutOrStdout().Write(append(out, '\n'))
		return werr
	}
	// writeExportFile (leaderboard.go) atomically writes to path, creating parents.
	return writeExportFile(output, out)
}

// warnVocabularyDiagnostics emits every vocabulary signal a finished run owes its
// operator: the run-level ceiling breach, then the per-reviewer rows that drifted.
//
// The two are joined at ONE call site rather than invoked separately from the command
// so the pairing is testable from a RunResult — the scenario that matters is precisely
// the one where the first signal is SILENT and the second is not, and a test that calls
// each helper directly cannot observe that they are both reached. Neither gates the
// other: a run can breach the ceiling and also have the breach concentrated in one row,
// and those are two different facts.
func warnVocabularyDiagnostics(w io.Writer, rr *benchmark.RunResult) {
	if rr == nil {
		return
	}
	warnIfVocabularyCeilingExceeded(w, rr.OutOfVocabularyRate)
	warnDriftingReviewers(w, rr.Vocabulary)
	warnRoutingOnlyReviewers(w, rr.Vocabulary)
}

// maxReviewerDriftRate is the per-reviewer out-of-vocabulary rate at or above which
// warnDriftingReviewers names a reviewer. The comparison is `*r.Rate >= maxReviewerDriftRate`
// — the same `>=` operator ExceedsVocabularyCeiling applies to the run-level ceiling, so
// a reviewer sitting exactly on this rate is named just as a run sitting exactly on the
// ceiling trips the guard.
//
// # Why this is NOT benchmark.MaxOutOfVocabularyRate
//
// Reusing the run-level ceiling here is the obvious move and the wrong one. That
// number is a RUN guard, tightened to 0.05 from a single valid observation (V1's
// 0.0100), and the epic that tightened it recorded the caveat explicitly: n=1, so
// variance under this metric is unmeasured. Applying an n=1-derived bound to
// individual rows assumes a tightness one observation cannot support — and a
// per-reviewer signal that fires on ordinary between-model variation reproduces
// exactly the defect warnIfVocabularyCeilingExceeded's own doc names: a warning
// printed on every run is a warning nobody reads.
//
// 0.50 is a qualitatively different claim — a MAJORITY of this reviewer's own findings
// missed a 32-word enumeration it was handed — rather than a quantitative reading of a
// distribution nobody has measured yet. The case this warning exists for (one reviewer
// at 100% drift hidden under a passing run rate) clears it by a factor of two.
//
// Tighten this once a second valid run makes the spread between models measurable.
// Until then a looser threshold costs a missed moderate drifter, while a tighter one
// costs the signal's credibility on every run.
const maxReviewerDriftRate = 0.50

// maxDriftWarningRows caps the per-reviewer drift listing. The realistic breach cause
// is a findings-parser regression, which drifts every reviewer at once — on a 27-model
// roster an uncapped listing is a wall of rows nobody reads, the outcome the threshold
// doc argues against. The header still quotes the true drifting count; the tail
// summarizes the capped remainder.
const maxDriftWarningRows = 10

// warnDriftingReviewers names individual reviewers whose own drift is severe enough to
// matter, and is the answer to the question the run-level warning structurally cannot
// answer: WHICH model ignored the vocabulary.
//
// The run-level rate is micro-averaged — correctly, since drift is a property of the
// run's findings — but that makes it concealing rather than merely coarse. A reviewer
// raising 12 findings that all drifted, pooled against a peer raising 300 clean ones,
// reports 12/312 = 0.038: under the ceiling, no warning, run reads clean, and one of
// two models never used the enumeration at all. This walks the per-reviewer breakdown
// so that reviewer is named.
//
// Tightening the ceiling does not remove the need for this. It raises the dilution a
// concealed drifter needs (at 0.20 the same 12 findings hid behind 80 clean ones;
// at 0.05 they need ~300) — it does not bound it, because the ratio is set by the
// roster's other reviewers, not by the guard.
//
// Both signals fire independently and neither suppresses the other: a run can breach
// the ceiling AND have the breach concentrated in one row, and those are two different
// facts an operator needs.
//
// Counts are quoted alongside the rate because a rate alone is unreadable at small n —
// 1/1 and 80/80 are both 1.00 and are not the same finding. That is deliberately in
// place of a minimum-findings floor, which would silently drop the small-n rows rather
// than let the operator judge them.
//
// Like the run-level warning this writes to STDERR and is deliberately NOT an
// exit-code change: `benchmark run --output <path>` prints nothing to stdout, and
// failing a multi-hour validation run at the end over a diagnostic would discard the
// work the run existed to produce.
func warnDriftingReviewers(w io.Writer, rows []benchmark.ReviewerVocabulary) {
	// A nil rate is UNMEASURED, not drifted — the same nil-vs-zero distinction the
	// pointer carries. Naming the reviewers of a total-failure run as the vocabulary
	// problem would misdiagnose a run that raised nothing to measure.
	drifting := make([]benchmark.ReviewerVocabulary, 0, len(rows))
	for _, r := range rows {
		if r.Rate != nil && *r.Rate >= maxReviewerDriftRate {
			drifting = append(drifting, r)
		}
	}
	if len(drifting) == 0 {
		return
	}

	// The realistic breach cause is a findings-parser regression, which drifts EVERY
	// reviewer at once — so the listing is severity-ordered and capped: the worst
	// drifters first, the remainder summarized, rather than an alphabetized wall.
	sort.Slice(drifting, func(i, j int) bool {
		if *drifting[i].Rate != *drifting[j].Rate {
			return *drifting[i].Rate > *drifting[j].Rate
		}
		return drifting[i].Findings > drifting[j].Findings
	})
	shown := drifting
	if len(shown) > maxDriftWarningRows {
		shown = shown[:maxDriftWarningRows]
	}

	noun := "reviewer"
	if len(drifting) > 1 {
		noun = "reviewers"
	}
	_, _ = fmt.Fprintf(w,
		"warning: %d %s labelled at least %.0f%% of their own findings with words outside the "+
			"offered vocabulary. The run-level out_of_vocabulary_rate pools every reviewer's "+
			"findings together, so a drifted reviewer measured against prolific clean peers can "+
			"leave it under the ceiling — read these rows, not just that number:\n",
		len(drifting), noun, maxReviewerDriftRate*100)
	for _, r := range shown {
		_, _ = fmt.Fprintf(w, "  %s/%s: %d/%d findings out of vocabulary (%.2f), %d routing values\n",
			r.Model, r.Persona, r.Drifted, r.Findings, *r.Rate, r.RoutingValues)
	}
	if rest := len(drifting) - len(shown); rest > 0 {
		_, _ = fmt.Fprintf(w, "  ...and %d more\n", rest)
	}
	_, _ = fmt.Fprintf(w,
		"Treat these rows' corroboration_rate as a measure of vocabulary agreement rather than "+
			"detection: a category outside the enumeration matches no expected category, so it "+
			"zeroes recall independently of what the reviewer actually found.\n")
}

// warnRoutingOnlyReviewers names reviewers who labelled EVERY finding with a routing
// value (`other` or `out-of-scope`) — the blind spot warnDriftingReviewers structurally
// cannot see. Both routing values are taxonomy members, so such a reviewer reports
// drift 0.0: identical, on the drift axis, to a reviewer that categorized every finding
// precisely, while conveying no categorical information at all.
//
// This is the operator-surface half of the discriminator ReviewerVocabulary.RoutingValues
// was added to provide: the run result computes it, and this warning is what keeps it
// from being discoverable only by hand-correlating two JSON arrays by index. The pairing
// to read is routing-only AND recall 0.0 — "categorized nothing" — against the
// positionally-aligned Reviewers row.
//
// Like the sibling warnings this writes to STDERR and is deliberately NOT an exit-code
// change.
func warnRoutingOnlyReviewers(w io.Writer, rows []benchmark.ReviewerVocabulary) {
	var routing []benchmark.ReviewerVocabulary
	for _, r := range rows {
		if r.Findings > 0 && r.RoutingValues == r.Findings {
			routing = append(routing, r)
		}
	}
	if len(routing) == 0 {
		return
	}

	noun := "reviewer"
	if len(routing) > 1 {
		noun = "reviewers"
	}
	_, _ = fmt.Fprintf(w,
		"warning: %d %s labelled every finding with a routing value (`other` or `out-of-scope`). "+
			"Routing values are taxonomy members, so their drift rate is 0.0 and no warning above "+
			"can see them — yet they conveyed no categorical information. Read these rows' recall "+
			"on the aligned reviewers breakdown, not their drift rate:\n",
		len(routing), noun)
	for _, r := range routing {
		_, _ = fmt.Fprintf(w, "  %s/%s: %d/%d findings labelled with routing values\n",
			r.Model, r.Persona, r.RoutingValues, r.Findings)
	}
}

// warnIfVocabularyCeilingExceeded emits an operator-visible warning when a run's
// measured out-of-vocabulary rate breaches benchmark.MaxOutOfVocabularyRate.
//
// It writes to STDERR deliberately: `benchmark run --output <path>` prints nothing
// to stdout, so the documented resumable invocation would otherwise give the
// operator no signal at all that the reviewers ignored the offered vocabulary.
//
// It is deliberately NOT an exit-code change. A V1 validation run is 2-5 hours of
// paid LLM work, and failing it at the very end over a diagnostic would discard
// that work for a number the run was executed to discover.
//
// A nil (unmeasured) or in-range rate is silent — a warning printed on every run is
// a warning nobody reads. It is also RUN-level and therefore cannot name a drifting
// reviewer; warnDriftingReviewers is the sibling that can.
func warnIfVocabularyCeilingExceeded(w io.Writer, rate *float64) {
	if !benchmark.ExceedsVocabularyCeiling(rate) {
		return
	}
	_, _ = fmt.Fprintf(w,
		"warning: out_of_vocabulary_rate %.2f is at or above the %.2f ceiling — "+
			"reviewers are labelling findings with words outside the offered vocabulary, "+
			"which zeroes their recall independently of what they actually detected. "+
			"Treat this run's corroboration_rate as a measure of vocabulary agreement, not detection.\n",
		*rate, benchmark.MaxOutOfVocabularyRate)
}
