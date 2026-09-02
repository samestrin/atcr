package mcp

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
	"github.com/samestrin/atcr/internal/verify"
	reclib "github.com/samestrin/atcr/reconcile"
)

// mcpAllowedFormats is the explicit allow list of report formats surfaced through
// the MCP atcr_report tool (Design Decision #3, AC 01-05). Modeling the MCP
// surface as an allow list — rather than report.FormatList() minus a hardcoded
// exclusion — means a future CLI-only format added to FormatList() is excluded
// by construction: it reaches the MCP enum only when deliberately added here.
// AXI is excluded deliberately: the entire point of --axi is to skip MCP's token
// overhead (axi.md's own benchmark: MCP costs 2.3x-12x more tokens than the
// CLI), so an axi-encoded string nested inside an MCP JSON-RPC envelope delivers
// none of that saving and would be misleading. The allow list lives here in the
// MCP layer, not in report.FormatList(), so the CLI's report.ValidFormat/Formats
// keep advertising axi while only the MCP enum omits it.
var mcpAllowedFormats = []string{
	report.FormatMarkdown,
	report.FormatJSON,
	report.FormatChecklist,
	report.FormatSarif,
}

// mcpReportFormats returns the report formats surfaced through the MCP atcr_report
// tool — the explicit mcpAllowedFormats allow list. The JSON Schema enum and
// handleReport's defense-in-depth guard consult this one set, so the two sites
// can never drift on which formats the MCP surface accepts.
func mcpReportFormats() []string {
	return mcpAllowedFormats
}

// mcpReportFormatsText is mcpReportFormats joined for description/help text, the
// MCP-facing analogue of report.Formats().
func mcpReportFormatsText() string {
	return strings.Join(mcpReportFormats(), ", ")
}

// Tool names — the public MCP contract. Renaming any of these is a breaking
// change that requires a coordinated v2 bump (AC 04-02).
const (
	ToolReview    = "atcr_review"
	ToolReconcile = "atcr_reconcile"
	ToolVerify    = "atcr_verify"
	ToolDebate    = "atcr_debate"
	ToolReport    = "atcr_report"
	ToolRange     = "atcr_range"
	ToolStatus    = "atcr_status"
	ToolMetrics   = "atcr_metrics"
)

// runningStatus is the status atcr_review returns immediately; the fan-out
// continues in the server process and completion is polled via atcr_status
// (AC 04-03 Scenario 4).
const runningStatus = "running"

// ReviewArgs are the atcr_review tool arguments. All fields are optional: a
// client may call with {} to review the auto-detected range of the current
// branch (AC 04-02 Edge Case 2).
type ReviewArgs struct {
	ID          string `json:"id,omitempty" jsonschema:"review id; defaults to <YYYY-MM-DD>_<branch-slug>"`
	Base        string `json:"base,omitempty" jsonschema:"base ref; auto-detected from the default branch when omitted"`
	Head        string `json:"head,omitempty" jsonschema:"head ref; defaults to HEAD"`
	MergeCommit string `json:"merge_commit,omitempty" jsonschema:"review a single merge commit (base = SHA^, head = SHA)"`
}

// ReconcileArgs are the atcr_reconcile tool arguments (all optional).
type ReconcileArgs struct {
	IDOrPath        string `json:"id_or_path,omitempty" jsonschema:"review id to reconcile (review id only; paths are not accepted); defaults to .atcr/latest"`
	FailOn          string `json:"fail_on,omitempty" jsonschema:"set pass=false if any finding at or above this severity survives: CRITICAL, HIGH, MEDIUM, or LOW"`
	RequireVerified bool   `json:"require_verified,omitempty" jsonschema:"with fail_on: count only skeptic-confirmed (VERIFIED) findings — the strictest gate; requires fail_on"`
	Consensus       string `json:"consensus,omitempty" jsonschema:"consensus filter level for uncorroborated singletons on a 3+ reviewer panel: strict (default), lenient (keep MEDIUM-confidence singletons), or off (filter inert)"`
	Repo            string `json:"repo,omitempty" jsonschema:"repository root whose .atcr/debt store reconciled findings are persisted to — must be an ABSOLUTE path carrying a .git or .atcr marker (a relative path such as \".\" is rejected: it would resolve against the server's working directory, not the reviewed repo); defaults to the repo root recorded in the review manifest, and persistence is skipped with a warning when neither is available"`
	NoLocalDebt     bool   `json:"no_local_debt,omitempty" jsonschema:"skip writing reconciled findings to the local TD store for this run (mirrors the CLI's --no-local-debt); the reconcile result is unaffected"`
}

// VerifyArgs are the atcr_verify tool arguments. id_or_path is the review id
// (paths are not accepted), defaulting to .atcr/latest. fresh re-verifies
// already-verified findings; thorough uses 3 skeptics with majority rule;
// min_severity is the floor below which findings keep their v1 confidence;
// fail_on / require_verified drive the returned gate status (require_verified
// requires fail_on).
type VerifyArgs struct {
	IDOrPath        string `json:"id_or_path,omitempty" jsonschema:"review id to verify (review id only; paths are not accepted); defaults to .atcr/latest"`
	Fresh           bool   `json:"fresh,omitempty" jsonschema:"re-verify findings that already carry a verdict"`
	Thorough        bool   `json:"thorough,omitempty" jsonschema:"use 3 skeptics per finding with majority rule (default 1)"`
	MinSeverity     string `json:"minSeverity,omitempty" jsonschema:"skip findings below this severity floor: CRITICAL, HIGH, MEDIUM, or LOW (default MEDIUM)"`
	RegistryPath    string `json:"registryPath,omitempty" jsonschema:"override the registry file path (default: the user/project merged registry)"`
	FailOn          string `json:"failOn,omitempty" jsonschema:"compute a gate status: not-passing if any finding at or above this severity survives verification"`
	RequireVerified bool   `json:"requireVerified,omitempty" jsonschema:"with failOn: gate counts only skeptic-confirmed (VERIFIED) findings; requires failOn"`
}

// DebateArgs are the atcr_debate tool arguments. id_or_path is the review id
// (paths are not accepted), defaulting to .atcr/latest. single_model opts in to
// the same-model persona fallback when fewer than three distinct models are
// available; fail_on / require_verified drive the returned gate status
// (require_verified requires fail_on).
type DebateArgs struct {
	IDOrPath        string `json:"id_or_path,omitempty" jsonschema:"review id to debate (review id only; paths are not accepted); defaults to .atcr/latest"`
	SingleModel     bool   `json:"singleModel,omitempty" jsonschema:"allow the same-model persona fallback when fewer than 3 distinct models are available across the proposer/challenger/judge roles"`
	RegistryPath    string `json:"registryPath,omitempty" jsonschema:"override the registry file path (default: the user/project merged registry)"`
	FailOn          string `json:"failOn,omitempty" jsonschema:"compute a gate status: not-passing if any finding at or above this severity survives the debate"`
	RequireVerified bool   `json:"requireVerified,omitempty" jsonschema:"with failOn: gate counts only confirmed (VERIFIED) findings; requires failOn"`
}

// DebateResult is the atcr_debate summary: the per-outcome tally, the recorded
// overflow count, the wall-clock duration, and — when failOn was given — the gate
// status. Artifacts (debate.json, re-emitted findings.json, transcripts) are
// always written regardless of the gate outcome.
type DebateResult struct {
	ReviewID   string      `json:"review_id"`
	Selected   int         `json:"selected"`
	Upheld     int         `json:"upheld"`
	Overturned int         `json:"overturned"`
	Split      int         `json:"split"`
	Unresolved int         `json:"unresolved"`
	Overflow   int         `json:"overflow"`
	DurationMs int         `json:"durationMs"`
	GateStatus *GateStatus `json:"gateStatus,omitempty"`
}

// ReportArgs are the atcr_report tool arguments (all optional). Format is
// constrained to a closed enum by reportInputSchema.
type ReportArgs struct {
	IDOrPath string `json:"id_or_path,omitempty" jsonschema:"review id to render (review id only; paths are not accepted); defaults to .atcr/latest"`
	Format   string `json:"format,omitempty" jsonschema:"output format (default md); see the format enum for supported values"`
}

// RangeArgs are the atcr_range tool arguments (all optional).
type RangeArgs struct {
	Base        string `json:"base,omitempty" jsonschema:"base ref; auto-detected from the default branch when omitted"`
	Head        string `json:"head,omitempty" jsonschema:"head ref; defaults to HEAD"`
	MergeCommit string `json:"merge_commit,omitempty" jsonschema:"resolve a single merge commit (base = SHA^, head = SHA)"`
}

// StatusArgs are the atcr_status tool arguments (all optional).
type StatusArgs struct {
	IDOrPath string `json:"id_or_path,omitempty" jsonschema:"review id to query (review id only; paths are not accepted); defaults to .atcr/latest"`
}

// MetricsArgs are the atcr_metrics tool arguments: none. The tool returns the
// whole in-process registry.
type MetricsArgs struct{}

// MetricsResult carries the rendered metrics and the format they are in
// (mirrors ReportResult). Format is always "prometheus".
type MetricsResult struct {
	Format  string `json:"format"`
	Content string `json:"content"`
}

// ReviewResult is returned by atcr_review immediately after the review directory
// is created and the fan-out has started; the run continues in the server
// process and completion is polled via atcr_status (AC 04-03).
type ReviewResult struct {
	ReviewID   string `json:"review_id"`
	ReviewPath string `json:"review_path"`
	Status     string `json:"status"`
	AgentCount int    `json:"agent_count"`
}

// ReconcileResult is the atcr_reconcile summary. Pass is false when a fail_on
// threshold was given and at least one surviving finding sits at or above it; in
// that case Findings carries the offending records so the client can render them
// inline without a follow-up atcr_report call (AC 04-03 Scenario 7).
type ReconcileResult struct {
	ReviewID      string `json:"review_id"`
	Pass          bool   `json:"pass"`
	TotalFindings int    `json:"total_findings"`
	Partial       bool   `json:"partial"`
	FailOn        string `json:"fail_on,omitempty"`
	Consensus     string `json:"consensus,omitempty"`
	// UnresolvedFiltered is the Tier 4 content-resolution count: findings routed
	// out of the primary stream into the unresolved sidecar. Not omitempty, for
	// the same reason as DebtPersisted — 0 is the answer worth transmitting,
	// because the field exists to explain a smaller TotalFindings.
	UnresolvedFiltered int `json:"unresolved_filtered"`
	// UnresolvedState is what the Tier 4 check actually DID, which the count above
	// cannot convey: a 0 is produced by several conditions and most of them mean
	// the check never adjudicated anything. The agentic path needs this exactly as
	// much as the CLI does — more so, since an agent reading a bare 0 will read it
	// as "no fabricated findings" when it may mean "never looked".
	//
	// NOT omitempty, for the same reason as UnresolvedFiltered above: the zero
	// value is the answer worth transmitting. An EMPTY state means content
	// resolution did not run at all for this reconcile, which is the ordinary
	// case here — the MCP server operates on a review-artifact dir and passes an
	// empty Root unless a store root resolves (see the Root comment in
	// reconcileHandler). Omitting the key would leave a client unable to tell
	// that from a check that ran; it is precisely the ambiguity this field
	// exists to remove. The published Summary keeps omitempty instead, because
	// its contract is byte-identical output for an embedder that never resolves
	// content — a different surface with a different, equally explicit rule.
	UnresolvedState string `json:"unresolved_state"`
	// Unresolved carries the routed records themselves — the same records
	// reconciled/unresolved.json persists — so a wrongly-routed finding is
	// discoverable from the tool result without opening the sidecar by hand
	// (Epic 35.16.6.5 TD). omitempty: an empty sidecar is the common case.
	//
	// Sourced from this run's own result, not from a re-read of the sidecar:
	// nothing serializes two atcr_reconcile calls on one review directory, and a
	// concurrent rewrite used to be able to empty this field while
	// UnresolvedFiltered stayed nonzero. It is therefore always consistent with
	// UnresolvedFiltered, whatever the on-disk sidecar says by the time the
	// result is assembled.
	Unresolved []reconcile.JSONFinding `json:"unresolved,omitempty"`
	// UnresolvedReadError says why the PERSISTED sidecar could not be read,
	// mirroring DebtSkippedReason below. The warning otherwise goes only to the
	// server logger a stdio client never sees.
	//
	// It no longer qualifies `Unresolved`, which is reported from memory and
	// survives an unreadable sidecar: a client can now receive both the records
	// and this error. What it says is narrower and still worth saying — the
	// artifact left on disk for a human, or for a later tool, is unreadable.
	//
	// It cannot fire from a concurrent atcr_reconcile: unresolved.json is
	// published atomically, so a racing run leaves a valid file. This reports
	// corruption from outside atcr — a disk fault, a lost permission, a
	// third-party writer. omitempty: the common case is a read that worked.
	UnresolvedReadError string                  `json:"unresolved_read_error,omitempty"`
	Findings            []reconcile.JSONFinding `json:"findings,omitempty"`
	// DebtPersisted reports whether the local TD store write ran against a
	// resolved root (individual findings may still be dropped by path
	// validation), and DebtSkippedReason says why it did not run at all (TD
	// internal/mcp/tools.go:169). Persistence is best-effort by contract, so a
	// skip cannot fail the tool — but the only trace used to be a line on the
	// server's stderr, which a stdio client never sees. Without these fields a
	// client that mistyped repo reads "pass=true, 12 findings" indefinitely while
	// nothing is ever written. DebtPersisted is not omitempty: "false" is the
	// answer worth transmitting.
	DebtPersisted     bool   `json:"debt_persisted"`
	DebtSkippedReason string `json:"debt_skipped_reason,omitempty"`
}

// GateStatus is the verify gate outcome, present in VerifyResult only when a
// failOn threshold was supplied. Pass is false when at least one non-refuted
// finding (or, under requireVerified, VERIFIED finding) sits at or above the
// threshold; FailingCount is how many.
type GateStatus struct {
	Pass         bool   `json:"pass"`
	FailingCount int    `json:"failingCount"`
	FailOn       string `json:"failOn,omitempty"`
}

// VerifyResult is the atcr_verify summary: the verdict tally, the number of
// findings sent through verification this run, the wall-clock duration, and —
// when failOn was given — the gate status (AC 04-03). Artifacts are always
// emitted to disk regardless of the gate outcome.
type VerifyResult struct {
	ReviewID          string               `json:"review_id"`
	VerdictCounts     verify.VerdictCounts `json:"verdictCounts"`
	FindingsProcessed int                  `json:"findingsProcessed"`
	DurationMs        int                  `json:"durationMs"`
	GateStatus        *GateStatus          `json:"gateStatus,omitempty"`
}

// ReportResult carries the rendered report content and the format it was
// rendered in (AC 04-04).
type ReportResult struct {
	Format  string `json:"format"`
	Content string `json:"content"`
}

// RangeResult is the resolved range (AC 04-04). An empty diff is reported as
// commit_count: 0, file_count: 0 (not an error) so a client can detect "nothing
// to review" without exception handling.
type RangeResult struct {
	Base          string `json:"base"`
	Head          string `json:"head"`
	CommitCount   int    `json:"commit_count"`
	FileCount     int    `json:"file_count"` // ignore-filtered count: excludes repo-root .gitignore/.atcrignore matches
	DetectionMode string `json:"detection_mode,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Shallow       bool   `json:"shallow"`
}

// StatusResult is the review progress shape (AC 04-04). It mirrors
// fanout.ReviewStatus so CLI and MCP report identical state.
type StatusResult = fanout.ReviewStatus

// Tool descriptions: each states the tool's effect and its optional arguments
// (AC 04-02 Story-Specific).
const (
	descReview = "Fan a git range out to the reviewer pool and start the review. " +
		"Returns immediately with {review_id, review_path, status:\"running\", agent_count}; " +
		"poll atcr_status for completion. Optional args: id, base, head, merge_commit (all optional; defaults to the current branch vs. the default branch)."
	descReconcile = "Merge findings from all sources of a review into deduplicated, confidence-scored results. " +
		"Reconciled findings are also appended to the reviewed repo's local TD store; the root is the repo argument, else the root recorded in the review manifest — there is no working-directory fallback, so a review whose manifest records no root persists nothing unless repo is given. " +
		"Optional args: id_or_path (review id only; paths are not accepted; defaults to the latest review), fail_on (CRITICAL|HIGH|MEDIUM|LOW; sets pass=false when a finding at or above it survives), require_verified (with fail_on: count only VERIFIED findings), consensus (strict|lenient|off; default strict), repo (ABSOLUTE path to the reviewed repo root; required when the review manifest records no root), no_local_debt (skip the store write for this run). " +
		"The result reports the outcome of that write as debt_persisted / debt_skipped_reason, and the outcome of the Tier 4 content check as unresolved_filtered, unresolved_state (applied|disabled|unavailable|incomplete — read it before the count, which is not self-interpreting), unresolved (the routed records), and unresolved_read_error (the persisted sidecar was unreadable; unresolved is unaffected)."
	descVerify = "Run adversarial skeptics over a review's reconciled findings and re-emit the artifacts with verdicts and confidence v2. " +
		"Runs after atcr_reconcile. Returns {review_id, verdictCounts, findingsProcessed, durationMs, gateStatus?}. " +
		"Optional args: id_or_path (review id only; defaults to the latest review), fresh, thorough, minSeverity (CRITICAL|HIGH|MEDIUM|LOW), failOn, requireVerified."
	descDebate = "Cross-examine a review's disputed findings (severity splits, gray-zone clusters, verification disagreements) through a bounded proposer/challenger/judge debate and integrate the rulings. " +
		"Runs after atcr_reconcile (and atcr_verify, for verification disagreements). Returns {review_id, selected, upheld, overturned, split, unresolved, overflow, durationMs, gateStatus?}. " +
		"Optional args: id_or_path (review id only; defaults to the latest review), singleModel, failOn (CRITICAL|HIGH|MEDIUM|LOW), requireVerified."
	descRange = "Resolve a git review range without calling any provider. " +
		"Returns {base, head, commit_count, file_count}. Optional args: base, head, merge_commit (defaults to the current branch vs. the default branch)."
	descStatus = "Report a review's fan-out progress. " +
		"Returns {review_id, status, agent_count, agents_done, agents_pending, partial}. Optional args: id_or_path (review id only; paths are not accepted; defaults to the latest review)."
	descMetrics = "Return the atcr in-process metrics (review/agent counts and latencies, API errors, findings) in Prometheus text exposition format, cumulative since the server started. " +
		"Returns {format:\"prometheus\", content}. No arguments. Local-only: do not expose the server publicly."
)

// descReport is a var (not const) so the embedded format list is derived from
// mcpReportFormatsText(), the MCP-facing (axi-excluded) format list — the single
// source of truth the enum below shares, so the description can never drift from
// what the enum accepts.
var descReport = "Render a view over a review's reconciled findings. " +
	"Optional args: id_or_path (review id only; paths are not accepted; defaults to the latest review), " +
	"format (" + mcpReportFormatsText() + "; default " + report.FormatMarkdown + ")."

// reportInputSchema builds the atcr_report input schema with the format property
// constrained to the closed enum md|json|checklist|sarif — mcpReportFormats(),
// i.e. report.FormatList() with FormatAXI filtered OUT (Design Decision #3,
// AC 01-05) — so an invalid or CLI-only format is rejected by JSON Schema
// validation before the handler runs (AC 04-04 Edge Case 2). The handler
// additionally defends with its own enum check (including an explicit axi reject).
func reportInputSchema() (*jsonschema.Schema, error) {
	s, err := jsonschema.For[ReportArgs](nil)
	if err != nil {
		// Coverage exclusion: jsonschema.For cannot fail for the statically-known,
		// JSON-representable ReportArgs struct, so this guard is unreachable defense
		// in depth; it exists only to keep buildServer fail-fast if ReportArgs ever
		// gains an inference-hostile field.
		return nil, fmt.Errorf("inferring atcr_report schema: %w", err)
	}
	if p := s.Properties["format"]; p != nil {
		formats := mcpReportFormats()
		p.Enum = make([]any, len(formats))
		for i, f := range formats {
			p.Enum[i] = f
		}
		p.Description = "output format (default " + report.FormatMarkdown + "): " + mcpReportFormatsText()
	}
	return s, nil
}

// reconcileInputSchema builds the atcr_reconcile input schema with the consensus
// property constrained to the closed level vocabulary from reclib.ConsensusLevels
// — the same source the CLI usage error and the handler's defense-in-depth check
// read, so the three can never drift — so an invalid value is rejected by JSON
// Schema validation before the handler runs. The handler check stays: it covers
// the config/registry tiers, which no argument schema can see.
func reconcileInputSchema() (*jsonschema.Schema, error) {
	s, err := jsonschema.For[ReconcileArgs](nil)
	if err != nil {
		// Coverage exclusion: same unreachable-defense rationale as
		// reportInputSchema above — jsonschema.For cannot fail for the
		// statically-known ReconcileArgs struct.
		return nil, fmt.Errorf("inferring atcr_reconcile schema: %w", err)
	}
	if p := s.Properties["consensus"]; p != nil {
		p.Enum = make([]any, len(reclib.ConsensusLevels()))
		for i, l := range reclib.ConsensusLevels() {
			p.Enum[i] = l
		}
	}
	return s, nil
}
