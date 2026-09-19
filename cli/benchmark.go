package cli

import (
	"encoding/json"
	"errors"
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
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/scorecard"
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

	// Route on the suite's own discriminator, exactly as runBenchmarkRun does.
	// verify is the author's only FREE validation path; leaving it standard-v1-only
	// while run executed both tiers meant a repo-state author could reach
	// LoadRepoState and validateRepoStatePublishableCaseIDs by no route except
	// `benchmark run`, which proceeds into a paid panel.
	suiteFormat, err := benchmark.DetectSuiteFormat(suitePath)
	if err != nil {
		return err
	}
	// EqualFold for the same reason run routes with it, and with the same limit: a
	// differently-cased discriminator is ROUTED as this tier so the loader can refuse
	// it precisely — it is not ACCEPTED as this tier. See the note at runBenchmarkRun.
	if strings.EqualFold(suiteFormat, benchmark.FormatRepoStateV1) {
		return verifyRepoStateSuite(cmd, suitePath)
	}

	m, err := benchmark.Load(suitePath)
	if err != nil {
		return err
	}
	// The SAME gate `benchmark run` applies at load (cli/benchmark_run.go). verify is
	// the suite-author pre-flight — its whole job is validation — so it must not
	// print "valid" for a manifest run hard-rejects seconds later. Wiring the gate
	// into run alone left the earliest surface an author touches as the one surface
	// that did not apply it.
	if err := validateSuitePublishableCaseIDs(m, suitePath); err != nil {
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
	// SuiteVersion takes %q like the adjacent Suite already does. Both come from a
	// third-party suite.json and Manifest.Validate only requires them non-blank, so an
	// escape sequence survives to the terminal under %s. Quoting one side and not the
	// other is the asymmetry that let it through.
	_, werr := fmt.Fprintf(cmd.OutOrStdout(),
		"suite %q version %q: %d %s, valid\nreproducibility hash: %s\n",
		m.Suite, m.SuiteVersion, len(m.Cases), noun, hash)
	return werr
}

// verifyRepoStateSuite is runBenchmarkVerify's repo-state arm: the SAME two gates
// executeRepoStateBenchmarkRun applies at load, in the same order, so verify and
// run cannot disagree about what a valid repo-state suite is.
//
// No reproducibility hash. ReproHashManifest is defined over a standard-v1
// *Manifest — it hashes each case's diff bytes — and there is no repo-state
// equivalent to call. Printing nothing where the standard tier prints a hash would
// read as a missing line, so the omission is NAMED rather than silent; inventing a
// second hashing contract here would put an unversioned one in the CLI, where the
// public submission format cannot see it.
func verifyRepoStateSuite(cmd *cobra.Command, suitePath string) error {
	m, err := benchmark.LoadRepoState(suitePath)
	if err != nil {
		return err
	}
	if err := validateRepoStatePublishableCaseIDs(m, suitePath); err != nil {
		return err
	}
	noun := "cases"
	if len(m.Cases) == 1 {
		noun = "case"
	}
	// %q on both identity fields, for the reason the standard arm's comment gives:
	// they come from a third-party suite.json and an escape sequence would otherwise
	// survive to the terminal.
	_, werr := fmt.Fprintf(cmd.OutOrStdout(),
		"suite %q version %q: %d %s, valid\nreproducibility hash: not defined for %s (standard-v1 only)\n",
		m.Suite, m.SuiteVersion, len(m.Cases), noun, benchmark.FormatRepoStateV1)
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
	cmd.Flags().Bool("fail-on-case-failure", false, "opt-in (repo-state-v1 only): exit non-zero when ANY case was lost to an infrastructure failure. Off by default, because a partial run is a real measurement of the cases that did run and the run-result records which ones did not — but a CI step gating on the exit code cannot see that, so this restores the all-or-nothing contract for callers that need it. Use --max-case-failures for a threshold instead of a floor of one. Inert on standard-v1, whose runner never populates case_failures.")
	cmd.Flags().Int("max-case-failures", -1, "opt-in (repo-state-v1 only): exit non-zero once MORE than this many cases were lost to infrastructure failures. -1 (default) means no ceiling. 0 is equivalent to --fail-on-case-failure. Set it to tolerate the occasional flaky provider while still failing a systemically broken run. Inert on standard-v1, whose runner never populates case_failures.")
	cmd.Flags().Int("max-consecutive-case-failures", 0, "opt-in (repo-state-v1 only): ABORT the run once this many cases have failed back to back, instead of paying for the rest of the suite. 0 (default) disables it. A scored case resets the count, so this stops a systemically broken provider — one bad key, a payload-size rejection — rather than the occasional flaky case. Unlike --fail-on-case-failure and --max-case-failures, which judge a run that has already been paid for in full, this one stops the bill mid-run.")
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
	failOnCaseFailure, _ := cmd.Flags().GetBool("fail-on-case-failure")
	maxCaseFailures, _ := cmd.Flags().GetInt("max-case-failures")
	maxConsecutiveCaseFailures, _ := cmd.Flags().GetInt("max-consecutive-case-failures")

	// Discover config the same way `atcr review` does (registry + project config
	// rooted at the cwd), so the benchmark roster is the project's reviewers.
	cfg, err := benchmarkLoadConfig(".")
	if err != nil {
		return err
	}

	// Route on the suite's own discriminator. The two tiers return the same
	// RunResult but reach it by different review paths — standard-v1 by diff
	// ingestion, repo-state-v1 by materializing a real repository and reviewing a
	// base..head range — so the choice cannot be deferred into a single loader.
	// Reading only the discriminator keeps a malformed repo-state suite producing a
	// repo-state error rather than a standard-v1 one.
	suiteFormat, err := benchmark.DetectSuiteFormat(suitePath)
	if err != nil {
		return err
	}
	if err := checkRepoStateFlags(suiteFormat, checkpoint); err != nil {
		return err
	}

	// Audit identity (Epic 35.0): a benchmark drives many models over many
	// cases, so without a stage those records are unattributable in a stream
	// shared with real review work.
	benchCtx := hookobs.WithCall(cmd.Context(), hookobs.Call{Stage: "benchmark"})
	var rr *benchmark.RunResult
	// Only the repo-state runner retains a work dir on a partial run; the standard
	// path leaves this empty and warnCaseFailures keeps its old wording.
	var retainedWorkDir string
	// Case-insensitive ROUTING, and what it buys is a precise ERROR — not acceptance
	// of the cased spelling. DetectSuiteFormat already TrimSpaces the discriminator;
	// an exact match here would send a manifest declaring "Repo-State-V1" to the
	// standard-v1 arm, which misses the known-other-format guard and dies on "diff
	// path is required", a message about a field the repo-state format never had.
	// Routed here instead, it reaches LoadRepoState, whose own EXACT tier check says
	// which spelling the manifest declared and which it must declare
	// (TestLoadRepoState_RejectsACasedDiscriminatorWithAnActionableError). The suite
	// is still refused either way; the difference is whether the operator is told why.
	//
	// The two rules are deliberately different: `suite` is a published format
	// contract compared literally wherever suite identity is compared, so the loader
	// accepts one spelling, while routing is only deciding which error to produce.
	isRepoState := strings.EqualFold(suiteFormat, benchmark.FormatRepoStateV1)
	runner := "executeBenchmarkRun"
	if isRepoState {
		runner = "executeRepoStateBenchmarkRun"
	}
	// Name the routing decision on stderr (via the context logger): an operator who
	// passed --suite-path must be able to tell from the log which tier actually
	// ran, without opening the run-result to check which metrics it carries.
	log.FromContext(benchCtx).Info("benchmark run: executing suite", "suite_format", suiteFormat, "runner", runner)
	if isRepoState {
		rr, retainedWorkDir, err = executeRepoStateBenchmarkRun(benchCtx, cfg, benchmarkNewCompleter(benchCtx), suitePath, time.Now().UTC(), maxConsecutiveCaseFailures)
	} else {
		rr, err = executeBenchmarkRun(benchCtx, cfg, benchmarkNewCompleter(benchCtx), suitePath, time.Now().UTC(), checkpoint)
	}
	if err != nil {
		return err
	}

	warnVocabularyDiagnostics(cmd.ErrOrStderr(), rr)
	// BEFORE the recall summary, because it qualifies it: a partial run's recall
	// covers only the cases that were scored, and a reader who sees the number first
	// has already taken it for a full-suite measurement.
	warnCaseFailures(cmd.ErrOrStderr(), rr, retainedWorkDir)
	warnPositionalRecallSummary(cmd.ErrOrStderr(), rr)

	data, err := json.MarshalIndent(rr, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding run-result: %w", err)
	}
	if out == "" {
		if _, werr := cmd.OutOrStdout().Write(append(data, '\n')); werr != nil {
			return werr
		}
	} else if werr := writeExportFile(out, data); werr != nil {
		// writeExportFile (leaderboard.go) atomically writes to path, creating parents.
		return werr
	}
	// LAST, after the run-result has been written. The strict gates change the EXIT
	// CODE, not the artifact: a partial run is a real measurement of the cases that
	// did run, and destroying it would make the opt-in cost the operator the paid work
	// as well as the exit status.
	return caseFailureExitGate(rr, failOnCaseFailure, maxCaseFailures)
}

// caseFailureExitGate implements the two opt-in strict exit contracts.
//
// The DEFAULT is unchanged and stays exit 0: a run that lost a case still measured
// the ones it kept, records which it did not in case_failures, and says so on stderr
// through warnCaseFailures. The only case that has always aborted is every case
// failing, which the runner handles itself.
//
// That default is wrong for one caller though — a CI step or wrapper script gating on
// the exit code, which cannot read a stderr warning and would silently accept a
// 1-of-100 measurement. These flags are for that caller and nobody else, which is why
// they are opt-in rather than a changed default: making the default strict would fail
// interactive runs that are behaving exactly as designed.
//
// --fail-on-case-failure is the floor-of-one; --max-case-failures is the same gate
// with a threshold, for a caller that tolerates the occasional flaky provider but not
// a systemically broken run. Both are evaluated, so passing both means either can
// fail the run.
func caseFailureExitGate(rr *benchmark.RunResult, failOnAny bool, maxFailures int) error {
	if rr == nil || len(rr.CaseFailures) == 0 {
		return nil
	}
	failed, suite := len(rr.CaseFailures), len(rr.SuiteCaseIDs)
	if failOnAny {
		return fmt.Errorf("%d of %d case(s) were lost to infrastructure failures and --fail-on-case-failure is set; "+
			"the run-result was still written and records which cases are missing", failed, suite)
	}
	if maxFailures >= 0 && failed > maxFailures {
		return fmt.Errorf("%d of %d case(s) were lost to infrastructure failures, more than the %d allowed by "+
			"--max-case-failures; the run-result was still written and records which cases are missing",
			failed, suite, maxFailures)
	}
	return nil
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
		// The version is named here, not only in the docs, for the same reason
		// leaderboard --export names it: this envelope is the one that GAINED keys
		// at submission_schema 2, and board acceptance of the new number is an
		// unverified coordination item. A benchmark submitter reads this help and
		// nothing else, so omitting it would leave the group most affected by the
		// risk uninformed. The number is formatted from the constant so one bump
		// updates every surface.
		Long: "Emit a suite-tagged public submission record from a benchmark run-result. " +
			fmt.Sprintf("The envelope stamps submission_schema %d; a board pinned to 1 must be updated to accept it.", scorecard.SubmissionSchema),
		Args: usageArgs(cobra.NoArgs),
		RunE: runBenchmarkExport,
	}
	cmd.Flags().String("in", "", "path to a benchmark run-result JSON file (produced by atcr benchmark run)")
	cmd.Flags().String("output", "", "write the submission JSON to this file instead of stdout (atomically replaces the target; a symlink at the path is replaced, not followed)")
	cmd.Flags().String("suite-path", "", "path to the suite directory (containing suite.json) the run-result was produced from. Optional: when given, the run-result's suite_case_ids must equal the manifest's case list, which anchors the coverage gate's denominator to the suite instead of to the file being checked. Without it the gate can only prove the file is internally consistent — a run-result that truncated its own case list passes.")
	cmd.Flags().Bool("allow-partial-coverage", false, "publish even when a reviewer row was scored over less than the full suite. Off by default: rows measured over different subsets of the suite are not comparable, and a mid-run model failover makes partial coverage a normal outcome rather than an exotic one. When set, "+partialCoverageVisibilityAdvisory+" — but a short row still is not comparable to a full one, which is why the gate stays closed by default.")
	_ = cmd.MarkFlagRequired("in")
	return cmd
}

// maxRunResultBytes caps the run-result read at export. The file is
// operator-supplied and every gate in benchmark_coverage.go walks it again, so its
// size multiplies through the whole export path — and it arrives having never passed
// through the producer, the same premise those gates are built on. The ceiling
// mirrors loadCheckpoint's maxCheckpointBytes (and fanout's readFileLimited), which
// is the same 32 MiB for the same reason one command over. It is a var, not a const,
// so tests can shrink it.
var maxRunResultBytes int64 = 32 << 20 // 32 MiB

// errRunResultTooLarge reports a run-result exceeding maxRunResultBytes. Export
// fails loudly rather than reading an untrusted file unbounded.
var errRunResultTooLarge = errors.New("run-result exceeds size limit")

// readRunResultLimited stats-and-caps before reading, then reads through a
// LimitReader anyway. The stat gives the useful diagnostic (it can name the actual
// size); the LimitReader closes the window in which the file grows between the two
// calls, which is the one shape a stat alone cannot cover.
func readRunResultLimited(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading run-result %s: %w", path, err)
	}
	if fi.Size() > maxRunResultBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes (limit %d)", errRunResultTooLarge, path, fi.Size(), maxRunResultBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading run-result %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	// One byte PAST the ceiling, so a file exactly at the limit still reads whole and
	// only an over-limit one is detectable by length.
	data, err := io.ReadAll(io.LimitReader(f, maxRunResultBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading run-result %s: %w", path, err)
	}
	if int64(len(data)) > maxRunResultBytes {
		return nil, fmt.Errorf("%w: %s grew past %d bytes while it was being read", errRunResultTooLarge, path, maxRunResultBytes)
	}
	return data, nil
}

func runBenchmarkExport(cmd *cobra.Command, _ []string) error {
	// Cobra GetString errors are unreachable: both flags are registered above
	// ("in" is MarkFlagRequired), so GetString returns the flag value or its
	// default, never an error. Project-wide convention (27 sites).
	in, _ := cmd.Flags().GetString("in")
	output, _ := cmd.Flags().GetString("output")
	allowPartial, _ := cmd.Flags().GetBool("allow-partial-coverage")
	suitePath, _ := cmd.Flags().GetString("suite-path")

	data, err := readRunResultLimited(in)
	if err != nil {
		return err
	}
	var rr benchmark.RunResult
	if err := json.Unmarshal(data, &rr); err != nil {
		return fmt.Errorf("parsing run-result %s: %w", in, err)
	}
	if strings.TrimSpace(rr.Suite) == "" || strings.TrimSpace(rr.SuiteVersion) == "" {
		return fmt.Errorf("run-result %s is missing suite/suite_version", in)
	}
	// Same seam as the reviewer-identity check below, one field over: the suite
	// identity is PUBLISHED (scrubbed, in BuildSubmission), so it owes the SAME
	// predicate validateScrubbedCaseIDs applies to the ids one field over — not just
	// the scrubs-to-empty half of it. The messages name the PRE-scrub strings under
	// %q, as the case-id diagnostics do.
	if err := validateSuiteIdentityForPublication(rr, in); err != nil {
		return err
	}
	if len(rr.Reviewers) == 0 {
		return fmt.Errorf("run-result %s has no reviewers", in)
	}
	// An unidentifiable reviewer row on a public leaderboard is worse than a
	// rejected file: same TrimSpace rule as the suite identity above.
	//
	// Checked on the SCRUBBED identity, because that is the value BuildSubmission
	// actually serializes (benchmark.go's defense-in-depth re-scrub). Checking the raw
	// one let a non-empty id that scrubs away pass the gate and publish as "": scrubField
	// iterates to a fixed point, so one pass can EXPOSE a match for an earlier rule —
	// "bedrock@us-east-1/claude" loses its email-shaped prefix to leave "/claude", which
	// the next pass reads as an absolute path. Nothing downstream catches it either, since
	// checkCoverage joins on the scrubbed identity and ("", persona) matches on both sides.
	//
	// The message names the PRE-scrub string deliberately: the scrubbed value is empty by
	// construction here, so reporting it would tell the operator only that something in
	// their file is empty, and never which row.
	for i, rev := range rr.Reviewers {
		// Printability is checked on the RAW identity, before the scrub, for the same
		// reason validateSuiteIdentityForPublication checks it first: ScrubPublicString
		// provably leaves control (Cc) and format (Cf) runes alone, so an invisible rune
		// survives into the published envelope and no arm below can see it. The empty-only
		// gate cannot reach it either — the value is non-empty on both sides.
		for _, f := range []struct{ name, value string }{
			{"model", rev.Model},
			{"persona", rev.Persona},
		} {
			if r, bad := firstNonPrintingRune(f.value); bad {
				return fmt.Errorf("run-result %s has reviewer %d with %s %q, which contains a non-printing rune (U+%04X); "+
					"control and format runes are invisible or reorder text in the published document, "+
					"so a leaderboard row can be misattributed to a model that was never measured",
					in, i, f.name, f.value, r)
			}
		}
		pub := scorecard.ScrubPublicRecord(rev)
		if strings.TrimSpace(pub.Model) == "" || strings.TrimSpace(pub.Persona) == "" {
			return fmt.Errorf("run-result %s has reviewer %d with empty model/persona once scrubbed for publication (%q/%q); "+
				"an identity that scrubs away publishes as \"\" on the leaderboard", in, i, rev.Model, rev.Persona)
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
	// The same gate, one array over. Wired directly beside its twin so the two
	// cannot drift on when they run.
	if err := validateReviewerPositionalRecall(cmd.ErrOrStderr(), rr, in); err != nil {
		return err
	}

	// Same seam as the reviewer-identity check above, one field over. As of
	// submission_schema 2 the case ids are PUBLISHED, and BuildSubmission scrubs them
	// on the way out — but every check below validates the RAW ids. Where the two
	// disagree, the document that ships means something no gate ever inspected.
	//
	// Runs BEFORE the anchor and the gate so their diagnostics are never the last word
	// on a file whose published form differs from the checked one.
	if err := validateScrubbedCaseIDs(rr, in); err != nil {
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
	// BEFORE the gate, not after: checkCoverage reads case_failures to explain a
	// shortfall, so an unvalidated entry would reach an operator-facing diagnostic —
	// and could attach an excuse to a row that never earned one — before anything
	// checked it was a reason the producer can write.
	if err := validateCaseFailures(rr, in); err != nil {
		return err
	}
	// Beside its sibling and for the identical reason: checkCoverage reads
	// slot_failures to explain a short reviewer row, so an unvalidated entry would
	// reach an operator-facing diagnostic — and could attach an excuse to a row that
	// never earned one — before anything checked the producer could have written it.
	if err := validateSlotFailures(rr, in); err != nil {
		return err
	}
	if err := checkCoverage(cmd.ErrOrStderr(), rr, in, allowPartial); err != nil {
		return err
	}

	generatedAt, err := time.Parse(time.RFC3339, rr.GeneratedAt)
	if err != nil {
		return fmt.Errorf("parsing generated_at %q: %w", rr.GeneratedAt, err)
	}
	sub := benchmark.BuildSubmission(rr, generatedAt)
	// Backstop only: the gates above already enforce everything Validate checks,
	// with sharper diagnostics keyed on the raw file. A failure here means
	// BuildSubmission drifted from its own documented invariants.
	//
	// This branch is therefore DELIBERATELY UNREACHABLE AND UNTESTED. Reaching it
	// requires BuildSubmission to be wrong, which no fixture can arrange from
	// outside — a run-result that would produce an invalid Submission is rejected
	// by the gates above first. Validate's own arms and diagnostics are pinned in
	// internal/benchmark (TestSubmission_Validate), which constructs the invalid
	// documents directly; the value here is the assertion that they never occur,
	// not a path anything exercises.
	if err := sub.Validate(); err != nil {
		return fmt.Errorf("internal: submission built from %s violates its own invariants: %w", in, err)
	}
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

// partialCoverageVisibilityAdvisory is the shared coverage-carriage clause of the
// --allow-partial-coverage opt-out: what a consumer can see when a short row
// publishes. One constant, two surfaces (the checkCoverage warning in
// benchmark_coverage.go and the flag help below), so a reword cannot drift them
// apart — the rule vocabularyAgreementAdvisory already established, applied to the
// pair that has now gone stale twice.
//
// "without reasons" is load-bearing, not decoration: the submission carries the
// SIZE of the shortfall (short case_ids sets against suite_case_ids) but nothing
// that says WHY any case is missing — case_failures is run-result-only per the
// epic 35.16.10.1 Clarifications (see RunResult.CaseFailures). Earlier wordings of
// this clause promised visibility the submission does not deliver; the clause now
// states the limit in the same breath as the visibility, and
// TestBuildSubmission_DoesNotPublishCaseFailures pins the wire side of it.
const partialCoverageVisibilityAdvisory = "the shortfall is carried into the submission, without reasons — " +
	"a consumer can compare each reviewer_coverage row's case_ids against suite_case_ids and see the row is short; " +
	"nothing in the submission says why a case is missing"

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
// Category Cf is dropped alongside Cc for a different threat with the same root: Cc
// (ESC, the C1 escapes) lets an upstream ERASE and rewrite the operator's line, while
// Cf (the bidi overrides U+202D/U+202E, the isolates, the zero-width formatters)
// lets it REORDER or hide what is rendered — a model name carrying U+202E displays
// everything after it reversed, so the warning names a reviewer that does not exist.
// Neither is caught upstream: scorecard.scrubField's only whitespace pass is
// strings.Fields over unicode.IsSpace, which matches no rune in either category.
func stripTerminalControlRunes(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
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
	// The MEASURED value is printed to four places and the ceiling with %g. Two places
	// were sized for the retired 0.20 ceiling; at 0.05 they collapse every rate in
	// [0.05, 0.055) onto the ceiling's own rendering, so the at-or-above boundary the
	// constant's doc makes load-bearing became unreadable from the message. %g keeps the
	// ceiling exact instead of padding it with trailing zeroes, and follows the constant
	// if it is tightened again to a value two places cannot express.
	_, _ = fmt.Fprintf(w,
		"warning: out_of_vocabulary_rate %.4f is at or above the %g ceiling — "+
			"reviewers are labelling findings with words outside the offered vocabulary, "+
			"which zeroes their recall independently of what they actually detected.\n",
		*rate, benchmark.MaxOutOfVocabularyRate)
	return true
}

// warnPositionalRecallSummary gives the repo-state tier's headline number an
// operator surface. `benchmark run --output <path>` prints nothing to stdout (the
// run-result goes to the file), so without this a whole-panel run shows a silent
// terminal and the operator must open the JSON to learn the run measured
// anything — the rationale warnVocabularyDiagnostics carries for the vocabulary
// signals, applied to the one number this tier exists to produce. stderr, like
// those warnings: stdout stays machine-readable. Non-fatal, deliberately not an
// exit-code change, and wired at the same single call site so the pairing is
// testable from a RunResult.
//
// A nil recall is UNMEASURED (the case set carries no findings of that half),
// not zero — named as such rather than printed as 0.0, the same nil-vs-zero
// distinction every recall pointer in this package carries.
func warnPositionalRecallSummary(w io.Writer, rr *benchmark.RunResult) {
	if rr == nil || len(rr.PositionalRecall) == 0 {
		return // standard-v1 runs carry none: silent, like the in-range vocabulary rate
	}
	var msg strings.Builder
	msg.WriteString("repo-state positional recall:\n")
	for _, p := range rr.PositionalRecall {
		model := stripTerminalControlRunes(p.Model)
		persona := stripTerminalControlRunes(p.Persona)
		if p.Recall == nil {
			fmt.Fprintf(&msg, "  %s/%s: recall unmeasured (no expected findings)\n", model, persona)
		} else {
			fmt.Fprintf(&msg, "  %s/%s: recall %.2f (%d/%d matched)",
				model, persona, *p.Recall, p.MatchedTotal, p.ExpectedTotal)
			if p.OutsideDiffRecall == nil {
				msg.WriteString("; outside_diff unmeasured (no outside_diff findings)\n")
			} else {
				fmt.Fprintf(&msg, "; outside_diff_recall %.2f (%d/%d matched)\n",
					*p.OutsideDiffRecall, p.MatchedOutsideDiff, p.ExpectedOutsideDiff)
			}
		}
	}
	// The caveat the metric's own field doc carries, on the one surface an operator
	// actually reads the number from. ReviewerPositionalRecall, score_repostate.go and
	// docs/benchmark.md all state it at length; this line printed the figure bare, so
	// a zero read as reviewer inattention when it may be gate attrition — and on this
	// tier, whose whole subject is out-of-diff findings, that is the likelier cause.
	//
	// Appended ONCE per summary rather than per row: it describes how the metric is
	// computed, which is identical for every reviewer, and repeating it would bury the
	// numbers it qualifies. The run's grounding_enabled tag is deliberately not
	// interpolated — it lives per reviewer on ReviewerCoverage, not as one run-level
	// value, so naming it here would need a join that this warning has no other reason
	// to do.
	if len(rr.PositionalRecall) > 0 {
		msg.WriteString("  note: an outside_diff_recall of 0.00 conflates \"never consulted unchanged " +
			"code\" with \"found it and the grounding gate discarded it\" — check each row's " +
			"reviewer_coverage.grounding_enabled before reading it as reviewer inattention\n")
	}
	_, _ = io.WriteString(w, msg.String())
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
// admit a hand-written file that rounded the quotient, and tight enough to catch a rate
// that describes no run at the denominators this metric actually sees — see the crossover
// below for where that stops holding. The gate exists to catch a rate that describes no
// run, not to demand the producer's full float precision from every submitter.
//
// 5e-3 admits TWO-decimal rounding, which is the precision a human actually writes:
// 1/3 as `0.33`, not `0.333333`. At the previous 1e-6 that file was a HARD error that
// rejected the whole submission — at a gate docs/benchmark.md explicitly invites
// hand-authoring for, and on an array that gates nothing and never reaches the
// submission envelope.
//
// It is far below any real contradiction AT SMALL DENOMINATORS, and only there: adjacent
// quotients k/N and (k±1)/N differ by 1/N, so the margin shrinks as N grows. At N ≈ 8 the
// gap is 0.125, twenty-five times this bound; at N = 100 it is 0.01, twice; and past
// N ≈ 200 it falls INSIDE the bound, so a row off by a whole finding is admitted. That
// crossover is not hypothetical — the V1 validation run this epic's ceiling was derived
// from raised 201 findings.
//
// Accepted deliberately rather than fixed by making the tolerance relative (a fraction of
// 1/Findings): this array gates nothing, is never carried into the submission envelope,
// and its purpose is to reject a rate that describes NO run rather than to audit a large
// one to the finding. State the limit rather than overstating the guarantee.
const vocabularyRateTolerance = 5e-3

// vocabularyRateEpsilon is the float64 slack that makes vocabularyRateTolerance an
// INCLUSIVE bound. It is not a second tolerance: it is twelve orders of magnitude below
// the first, far too small to admit any difference the bound itself rejects, and exists
// only so a difference that is mathematically equal to the tolerance is not rejected for
// being a few ULP above it after the subtraction.
const vocabularyRateEpsilon = 1e-9

// validateReviewerPositionalRecall is validateReviewerVocabulary for the positional
// array, applying the same rules to the same shape for the same reason.
//
// The asymmetry it closes: reviewer_vocabulary gets seven arithmetic arms and two
// alignment warnings on the stated grounds that a run-result is untrusted
// hand-supplied input, and out_of_vocabulary_rate gets a hard range check — while
// reviewer_positional_recall, structurally identical (counts beside an optional
// rate, joined positionally to rr.Reviewers), got nothing. A file carrying
// matched_total 99 against expected_total 1, recall 42.0 and outside_diff_recall
// -3.0 exported with exit 0 and no warning.
//
// "AC5 holds because BuildSubmission never copies the field" is not a reason to
// skip it: that is equally true of reviewer_vocabulary, which is validated anyway.
// The run-result is a published artifact in its own right.
//
// Counts and rates FAIL; the positional join only WARNS — the same split the
// vocabulary validator makes, and for the same reason: an impossible number
// describes no run, whereas a misaligned array is unreadable but harmless on a path
// no consumer reads.
func validateReviewerPositionalRecall(w io.Writer, rr benchmark.RunResult, path string) error {
	if len(rr.PositionalRecall) == 0 {
		return nil
	}
	for i, p := range rr.PositionalRecall {
		// The three (expected, matched, rate) triples are checked by one loop rather
		// than three copies: the total and its two halves obey identical rules, and
		// spelling them out three times is how one of them ends up with a weaker gate.
		for _, part := range []struct {
			field             string
			expected, matched int
			rate              *float64
		}{
			{"expected_total/matched_total", p.ExpectedTotal, p.MatchedTotal, p.Recall},
			{"expected_outside_diff/matched_outside_diff", p.ExpectedOutsideDiff, p.MatchedOutsideDiff, p.OutsideDiffRecall},
			{"expected_within_diff/matched_within_diff", p.ExpectedWithinDiff, p.MatchedWithinDiff, p.WithinDiffRecall},
		} {
			if part.expected < 0 || part.matched < 0 {
				return fmt.Errorf("run-result %s has reviewer_positional_recall[%d] with negative %s (%d/%d)",
					path, i, part.field, part.expected, part.matched)
			}
			if part.matched > part.expected {
				return fmt.Errorf("run-result %s has reviewer_positional_recall[%d] with matched %d exceeding expected %d (%s) — "+
					"a numerator larger than its own denominator describes no run", path, i, part.matched, part.expected, part.field)
			}
			if part.rate == nil {
				// nil is UNMEASURED (this reviewer's suite planted no such finding),
				// never a defect — the nil-vs-zero distinction the pointer carries.
				continue
			}
			// A rate on a ZERO denominator is that distinction collapsed. ScorePositional
			// leaves the pointer nil when nothing was expected, so a value here publishes
			// an unmeasured half as measured — and on THIS array that is the headline
			// out-of-diff number.
			if part.expected == 0 {
				return fmt.Errorf("run-result %s has reviewer_positional_recall[%d] with a rate of %v for %s but expected nothing; "+
					"an unmeasured half carries no rate at all, so this row describes no run", path, i, *part.rate, part.field)
			}
			// Range-checked before the quotient comparison, for the reason the vocabulary
			// validator states: NaN compares false against the tolerance too, so this arm
			// is what rejects it.
			if r := *part.rate; math.IsNaN(r) || r < 0 || r > 1 {
				return fmt.Errorf("run-result %s has reviewer_positional_recall[%d] rate %v for %s outside [0,1]", path, i, r, part.field)
			}
			// Same tolerance and same inclusive-bound epsilon as the vocabulary rate: the
			// bound exists to admit an honestly-rounded hand-authored value while
			// rejecting a rate that contradicts its own counts.
			if want := float64(part.matched) / float64(part.expected); math.Abs(*part.rate-want)-vocabularyRateTolerance > vocabularyRateEpsilon {
				return fmt.Errorf("run-result %s has reviewer_positional_recall[%d] rate %v for %s that does not match its own "+
					"counts (%d/%d = %v)", path, i, *part.rate, part.field, part.matched, part.expected, want)
			}
		}
	}

	// Cross-row denominator equality. The slot skip removes a case's expected
	// findings from the reviewer whose slot failed, so two rows can carry different
	// ExpectedTotal/ExpectedOutsideDiff while each stays internally consistent —
	// "cover every expected finding in the suite" stopped being a property of every
	// row the moment slots could fail, and warnPositionalRecallSummary prints the
	// rows side by side. WARN, not FAIL: a differing denominator describes a real
	// run — only an impossible number fails here.
	if len(rr.PositionalRecall) > 1 {
		first := rr.PositionalRecall[0]
		for _, p := range rr.PositionalRecall[1:] {
			if p.ExpectedTotal != first.ExpectedTotal || p.ExpectedOutsideDiff != first.ExpectedOutsideDiff ||
				p.ExpectedWithinDiff != first.ExpectedWithinDiff {
				_, _ = fmt.Fprintf(w, "warning: run-result %s has reviewer_positional_recall rows with differing denominators "+
					"(%s/%s: %d expected (%d outside_diff) vs %s/%s: %d expected (%d outside_diff)) — the slot skip makes a row's "+
					"denominator reviewer-dependent, so the rows are not comparable on rate alone; compare only rows with equal expected counts\n",
					path, first.Model, first.Persona, first.ExpectedTotal, first.ExpectedOutsideDiff,
					p.Model, p.Persona, p.ExpectedTotal, p.ExpectedOutsideDiff)
				break
			}
		}
	}

	if len(rr.PositionalRecall) != len(rr.Reviewers) {
		_, _ = fmt.Fprintf(w, "warning: run-result %s has %d reviewer_positional_recall row(s) against %d reviewer(s); "+
			"the array documents a positional join (entry i describes reviewers[i]) that this file cannot satisfy. "+
			"Publishing anyway — no consumer reads it on this path.\n", path, len(rr.PositionalRecall), len(rr.Reviewers))
		return nil
	}
	for i, p := range rr.PositionalRecall {
		if p.Model != rr.Reviewers[i].Model || p.Persona != rr.Reviewers[i].Persona {
			// %q, NOT stripTerminalControlRunes — see the identical warning in
			// validateReviewerVocabulary: this reports a COMPARISON, and stripping
			// sanitizes by deletion, so two identities differing only by a control rune
			// would print as the same text.
			_, _ = fmt.Fprintf(w, "warning: run-result %s has reviewer_positional_recall[%d] (%q/%q) misaligned with "+
				"reviewers[%d] (%q/%q); the documented positional join does not hold. Publishing anyway — "+
				"no consumer reads it on this path.\n",
				path, i,
				p.Model, p.Persona, i,
				rr.Reviewers[i].Model, rr.Reviewers[i].Persona)
			return nil
		}
	}
	return nil
}

func validateReviewerVocabulary(w io.Writer, rr benchmark.RunResult, path string) error {
	if len(rr.Vocabulary) == 0 {
		return nil
	}
	for i, v := range rr.Vocabulary {
		if v.Findings < 0 || v.Drifted < 0 {
			// Rendered in the order the label names them. Transposed, this told the
			// operator the wrong field was negative — and its two siblings below get
			// their order right, which is what made the odd one out read as authoritative.
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] with negative findings/drifted (%d/%d)",
				path, i, v.Findings, v.Drifted)
		}
		if v.Drifted > v.Findings {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] with drifted %d exceeding findings %d — "+
				"a numerator larger than its own denominator describes no run", path, i, v.Drifted, v.Findings)
		}
		// RoutingValues counts against the SAME denominator as Drifted, so it gets the
		// same arithmetic gate. It is not decoration: warnRoutingOnlyReviewers keys on
		// RoutingValues == Findings exactly, so a row claiming MORE routed findings than
		// findings is both impossible and silently suppresses the all-`other` warning it
		// should have triggered.
		if v.RoutingValues < 0 || v.RoutingValues > v.Findings {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] with routing_values %d outside 0..findings (%d)",
				path, i, v.RoutingValues, v.Findings)
		}
		// Routing values (`other`, `out-of-scope`) are taxonomy MEMBERS, so a routed
		// finding is in vocabulary by definition and cannot also be drift. Both counts at
		// the denominator therefore contradict each other, and the two operator warnings
		// would fire on one row with opposite diagnoses.
		if v.Findings > 0 && v.Drifted == v.Findings && v.RoutingValues == v.Findings {
			return fmt.Errorf("run-result %s has reviewer_vocabulary[%d] with every finding counted as BOTH drifted "+
				"and routed (%d of %d); routing values are taxonomy members, so a finding cannot both "+
				"be one and be out of vocabulary", path, i, v.Findings, v.Findings)
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
		//
		// Compared with an ULP slack rather than a strict `>`: a two-decimal rounding's
		// WORST case is an error of exactly the tolerance, and in float64 that difference
		// lands a few ULP above the bound (|0.13-0.125| computes as 0.00500000000000000444).
		// A strict `>` therefore rejected the very rounding the bound was widened to admit,
		// and for an eighth denominator it rejected BOTH available two-decimal values —
		// no legal hand-authored rate existed for `findings: 8`, the doc's own example.
		// Widening the constant again would not have fixed that; the boundary itself is
		// what has to be inclusive.
		if want := float64(v.Drifted) / float64(v.Findings); math.Abs(*v.Rate-want)-vocabularyRateTolerance > vocabularyRateEpsilon {
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
			// %q, NOT stripTerminalControlRunes — this warning reports a COMPARISON, and
			// stripping sanitizes by deletion. The gate above is a raw != with no
			// trimming and unicode.IsControl covers \r/\n as well as ESC, so two
			// identities differing only by a control rune would print as the same text:
			// a warning that says the join is broken between two rows it renders
			// identically. %q is terminal-safe AND keeps the difference legible, the
			// same reason the suite-identity mismatch in benchmark_coverage.go uses it.
			_, _ = fmt.Fprintf(w, "warning: run-result %s has reviewer_vocabulary[%d] (%q/%q) misaligned with "+
				"reviewers[%d] (%q/%q); the documented positional join does not hold. Publishing anyway — "+
				"no consumer reads it on this path.\n",
				path, i,
				v.Model, v.Persona, i,
				rr.Reviewers[i].Model, rr.Reviewers[i].Persona)
			return nil
		}
	}
	return nil
}
