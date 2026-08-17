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
	"unicode"

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

// Injection seams for runBenchmarkRun's two process-level dependencies: config
// discovery rooted at the cwd, and a live LLM completer. Hard-called, they made the
// command's own body unreachable from a test — so its one wiring of the vocabulary
// diagnostics could be deleted with the suite staying green, which is the gap
// TestBenchmarkRunCmd_VocabularyDiagnosticsReachStderr closes.
//
// Package-level vars rather than parameters because the RunE signature is cobra's;
// tests swap them and restore via t.Cleanup. Production never reassigns them.
var (
	benchmarkLoadConfig = func(root string) (*fanout.ReviewConfig, error) {
		return fanout.LoadReviewConfig(root, registry.CLIOverrides{})
	}
	benchmarkNewCompleter = newCompleter
)

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
	cfg, err := benchmarkLoadConfig(".")
	if err != nil {
		return err
	}

	// Audit identity (Epic 35.0): a benchmark drives many models over many
	// cases, so without a stage those records are unattributable in a stream
	// shared with real review work.
	benchCtx := hookobs.WithCall(cmd.Context(), hookobs.Call{Stage: "benchmark"})
	rr, err := executeBenchmarkRun(benchCtx, cfg, benchmarkNewCompleter(benchCtx), suitePath, time.Now().UTC(), checkpoint)
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
	if err := validateReviewerVocabulary(cmd.ErrOrStderr(), rr, in); err != nil {
		return err
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
// operator: the run-level ceiling breach, then the per-reviewer rows that drifted,
// then any all-routing reviewers, and finally the shared advisory — ONCE, here, rather
// than carried as a trailing literal inside each helper. The common case is more than
// one signal firing, and two independent string literals drift into two subtly
// different instructions on the same stderr stream.
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
	fired := warnIfVocabularyCeilingExceeded(w, rr.OutOfVocabularyRate)
	fired = warnDriftingReviewers(w, rr.Vocabulary) || fired
	fired = warnRoutingOnlyReviewers(w, rr.Vocabulary) || fired
	if fired {
		_, _ = io.WriteString(w, vocabularyAgreementAdvisory)
	}
}

// vocabularyAgreementAdvisory is the shared trailing advisory of the vocabulary
// warnings: how to READ corroboration_rate on a run any of the signals fired on. One
// constant, one emission site (warnVocabularyDiagnostics), so a reword cannot produce
// two subtly different instructions on the same stream.
const vocabularyAgreementAdvisory = "Treat corroboration_rate as a measure of vocabulary " +
	"agreement rather than detection: a category outside the enumeration matches no expected " +
	"category, so it zeroes recall independently of what the reviewer actually found.\n"

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
// Why the micro-averaged run-level rate conceals such a reviewer, and why tightening
// the ceiling does not remove the need for this: see benchmark.PerReviewerVocabulary,
// which states that argument once and is the place to change it.
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
func warnDriftingReviewers(w io.Writer, rows []benchmark.ReviewerVocabulary) bool {
	// A nil rate is UNMEASURED, not drifted — the same nil-vs-zero distinction the
	// pointer carries. Naming the reviewers of a total-failure run as the vocabulary
	// problem would misdiagnose a run that raised nothing to measure.
	drifting := make([]benchmark.ReviewerVocabulary, 0, len(rows))
	for _, r := range rows {
		if benchmark.ExceedsReviewerDriftRate(r.Rate) {
			drifting = append(drifting, r)
		}
	}
	if len(drifting) == 0 {
		return false
	}

	// The realistic breach cause is a findings-parser regression, which drifts EVERY
	// reviewer at once — so the listing is severity-ordered and capped: the worst
	// drifters first, the remainder summarized, rather than an alphabetized wall.
	// SliceStable, not Slice: the comparator can still tie (equal rate AND equal
	// findings), and an unstable sort is free to order those two rows differently
	// between runs on byte-identical input. Score and PerReviewerVocabulary both use
	// the stable form for exactly that reason; this listing is diffed by operators the
	// same way, so it gets the same guarantee.
	sort.SliceStable(drifting, func(i, j int) bool {
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
	// Assemble the whole message and write it ONCE: this is a multi-part warning
	// (header, rows, trailer), and a stderr failure mid-sequence — closed pipe, full
	// disk — would otherwise land the header while the rows it promises silently
	// vanish. All-or-nothing, and still deliberately non-fatal on write error.
	var msg strings.Builder
	fmt.Fprintf(&msg,
		"warning: %d %s labelled at least %.0f%% of their own findings with words outside the "+
			"offered vocabulary. The run-level out_of_vocabulary_rate pools every reviewer's "+
			"findings together, so a drifted reviewer measured against prolific clean peers can "+
			"leave it under the ceiling — read these rows, not just that number:\n",
		len(drifting), noun, benchmark.MaxReviewerDriftRate*100)
	for _, r := range shown {
		fmt.Fprintf(&msg, "  %s/%s: %d/%d findings out of vocabulary (%.2f), %d routing values\n",
			stripTerminalControlRunes(r.Model), stripTerminalControlRunes(r.Persona),
			r.Drifted, r.Findings, *r.Rate, r.RoutingValues)
	}
	if rest := len(drifting) - len(shown); rest > 0 {
		fmt.Fprintf(&msg, "  ...and %d more\n", rest)
	}
	_, _ = io.WriteString(w, msg.String())
	return true
}

// stripTerminalControlRunes drops non-printable control runes (ESC, BEL, BACKSPACE, …)
// from a realized reviewer identity before it is written to a terminal. Model/Persona
// are provider/proxy-reported strings, and the only sanitizer upstream on this path
// (scorecard.scrubField) collapses unicode.IsSpace only — ESC survives it byte-for-byte,
// so a compromised or hostile upstream could otherwise erase and rewrite the operator's
// terminal line, including forging a reassuring line over the warning itself.
func stripTerminalControlRunes(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
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
func warnRoutingOnlyReviewers(w io.Writer, rows []benchmark.ReviewerVocabulary) bool {
	var routing []benchmark.ReviewerVocabulary
	for _, r := range rows {
		if r.Findings > 0 && r.RoutingValues == r.Findings {
			routing = append(routing, r)
		}
	}
	if len(routing) == 0 {
		return false
	}

	noun := "reviewer"
	if len(routing) > 1 {
		noun = "reviewers"
	}
	// Assembled and written ONCE, like warnDriftingReviewers: a multi-part warning that
	// lands its header and loses its rows to a stderr failure is worse than no warning.
	var msg strings.Builder
	fmt.Fprintf(&msg,
		"warning: %d %s labelled every finding with a routing value (`other` or `out-of-scope`). "+
			"Routing values are taxonomy members, so their drift rate is 0.0 and no warning above "+
			"can see them — yet they conveyed no categorical information. Read these rows' recall "+
			"on the aligned reviewers breakdown, not their drift rate:\n",
		len(routing), noun)
	for _, r := range routing {
		fmt.Fprintf(&msg, "  %s/%s: %d/%d findings labelled with routing values\n",
			stripTerminalControlRunes(r.Model), stripTerminalControlRunes(r.Persona),
			r.RoutingValues, r.Findings)
	}
	_, _ = io.WriteString(w, msg.String())
	return true
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
//
// The corroboration_rate advisory is deliberately NOT appended here: it is shared with
// the per-reviewer warnings and emitted once by warnVocabularyDiagnostics
// (vocabularyAgreementAdvisory) when any signal fired.
func warnIfVocabularyCeilingExceeded(w io.Writer, rate *float64) bool {
	if !benchmark.ExceedsVocabularyCeiling(rate) {
		return false
	}
	_, _ = fmt.Fprintf(w,
		"warning: out_of_vocabulary_rate %.2f is at or above the %.2f ceiling — "+
			"reviewers are labelling findings with words outside the offered vocabulary, "+
			"which zeroes their recall independently of what they actually detected.\n",
		*rate, benchmark.MaxOutOfVocabularyRate)
	return true
}

// validateReviewerVocabulary checks the reviewer_vocabulary diagnostic array on an
// untrusted (possibly hand-supplied) run-result at the export boundary.
//
// The two failure classes get deliberately different severities:
//
//   - A malformed ROW VALUE — a rate outside [0,1] or NaN, or drifted exceeding its
//     own denominator — is a HARD ERROR, matching the out_of_vocabulary_rate guard
//     immediately above. Those are not pessimistic readings; they are arithmetic that
//     cannot describe any run, and "no consumer reads it yet" did not exempt the
//     scalar either.
//
//   - A LENGTH or ORDER mismatch against Reviewers is a non-blocking WARNING. The
//     positional join (entry i describes Reviewers[i]) is what the field doc tells
//     consumers to rely on, but nothing on this path reads it today, and the array is
//     legitimately absent on any pre-field run-result. Rejecting there would add a new
//     bounce at a publication gate for a defect that currently misleads no one.
//
// An absent or empty array is silent: omission is the normal shape, not a defect.
//
// vocabularyRateTolerance is how far a published rate may sit from Drifted/Findings
// before the row is rejected as self-contradictory. It is deliberately loose enough to
// admit a hand-written file that rounded the quotient and far tighter than any real
// contradiction — the gate exists to catch a rate that describes no run, not to demand
// the producer's full float precision from every submitter.
const vocabularyRateTolerance = 1e-6

func validateReviewerVocabulary(w io.Writer, rr benchmark.RunResult, path string) error {
	if len(rr.Vocabulary) == 0 {
		return nil
	}
	for i, v := range rr.Vocabulary {
		if v.Findings < 0 || v.Drifted < 0 {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] with negative findings/drifted (%d/%d)",
				path, i, v.Drifted, v.Findings)
		}
		if v.Drifted > v.Findings {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] with drifted %d exceeding findings %d — "+
				"a numerator larger than its own denominator describes no run", path, i, v.Drifted, v.Findings)
		}
		if v.Rate == nil {
			// nil is UNMEASURED (this reviewer raised nothing), never a defect — the
			// same nil-vs-zero distinction the pointer carries everywhere else.
			continue
		}
		// A rate on a ZERO denominator is that same distinction collapsed. The producer
		// leaves Rate nil when Findings is 0 (benchmark.PerReviewerVocabulary), so a
		// value here publishes a reviewer that found nothing as measured-and-clean —
		// the most drifted possible row wearing a flawless number.
		if v.Findings == 0 {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] with a rate of %v but raised no findings; "+
				"an unmeasured reviewer carries no rate at all, so this row describes no run", path, i, *v.Rate)
		}
		if r := *v.Rate; math.IsNaN(r) || r < 0 || r > 1 {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] rate %v outside [0,1]", path, i, r)
		}
		// The rate must be the quotient it claims to be. PerReviewerVocabulary writes
		// exactly Drifted/Findings, so a rate contradicting its own counts cannot have
		// come from a run — and the rate, not the counts, is what a leaderboard reads.
		//
		// The tolerance admits a hand-assembled but honest file that rounded the value
		// (1/3 as 0.333333); it is far tighter than any contradiction worth catching.
		// Range-checked first on purpose: NaN compares false against this bound too, so
		// the check above is what rejects it.
		if want := float64(v.Drifted) / float64(v.Findings); math.Abs(*v.Rate-want) > vocabularyRateTolerance {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] rate %v that does not match its own "+
				"counts (%d/%d = %v)", path, i, *v.Rate, v.Drifted, v.Findings, want)
		}
	}

	if len(rr.Vocabulary) != len(rr.Reviewers) {
		_, _ = fmt.Fprintf(w, "warning: run-result %s has %d reviewer_vocabulary row(s) against %d reviewer(s); "+
			"the array documents a positional join (entry i describes reviewers[i]) that this file cannot satisfy. "+
			"Publishing anyway — no consumer reads it on this path.\n", path, len(rr.Vocabulary), len(rr.Reviewers))
		return nil
	}
	for i, v := range rr.Vocabulary {
		if v.Model != rr.Reviewers[i].Model || v.Persona != rr.Reviewers[i].Persona {
			_, _ = fmt.Fprintf(w, "warning: run-result %s has reviewer_vocabulary[%d] (%s/%s) misaligned with "+
				"reviewers[%d] (%s/%s); the documented positional join does not hold. Publishing anyway — "+
				"no consumer reads it on this path.\n",
				path, i,
				stripTerminalControlRunes(v.Model), stripTerminalControlRunes(v.Persona), i,
				stripTerminalControlRunes(rr.Reviewers[i].Model), stripTerminalControlRunes(rr.Reviewers[i].Persona))
			return nil
		}
	}
	return nil
}
