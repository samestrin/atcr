package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
)

// Tool names — the public MCP contract. Renaming any of these is a breaking
// change that requires a coordinated v2 bump (AC 04-02).
const (
	ToolReview    = "atcr_review"
	ToolReconcile = "atcr_reconcile"
	ToolReport    = "atcr_report"
	ToolRange     = "atcr_range"
	ToolStatus    = "atcr_status"
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
	IDOrPath string `json:"id_or_path,omitempty" jsonschema:"review id to reconcile (review id only; paths are not accepted); defaults to .atcr/latest"`
	FailOn   string `json:"fail_on,omitempty" jsonschema:"set pass=false if any finding at or above this severity survives: CRITICAL, HIGH, MEDIUM, or LOW"`
}

// ReportArgs are the atcr_report tool arguments (all optional). Format is
// constrained to a closed enum by reportInputSchema.
type ReportArgs struct {
	IDOrPath string `json:"id_or_path,omitempty" jsonschema:"review id to render (review id only; paths are not accepted); defaults to .atcr/latest"`
	Format   string `json:"format,omitempty" jsonschema:"output format: md (default), json, or checklist"`
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
	ReviewID      string                  `json:"review_id"`
	Pass          bool                    `json:"pass"`
	TotalFindings int                     `json:"total_findings"`
	Partial       bool                    `json:"partial"`
	FailOn        string                  `json:"fail_on,omitempty"`
	Findings      []reconcile.JSONFinding `json:"findings,omitempty"`
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
	FileCount     int    `json:"file_count"`
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
		"Optional args: id_or_path (review id only; paths are not accepted; defaults to the latest review), fail_on (CRITICAL|HIGH|MEDIUM|LOW; sets pass=false when a finding at or above it survives)."
	descReport = "Render a view over a review's reconciled findings. " +
		"Optional args: id_or_path (review id only; paths are not accepted; defaults to the latest review), format (md|json|checklist; default md)."
	descRange = "Resolve a git review range without calling any provider. " +
		"Returns {base, head, commit_count, file_count}. Optional args: base, head, merge_commit (defaults to the current branch vs. the default branch)."
	descStatus = "Report a review's fan-out progress. " +
		"Returns {review_id, status, agent_count, agents_done, agents_pending, partial}. Optional args: id_or_path (review id only; paths are not accepted; defaults to the latest review)."
)

// reportInputSchema builds the atcr_report input schema with the format property
// constrained to the closed enum md|json|checklist, so an invalid format is
// rejected by JSON Schema validation before the handler runs (AC 04-04 Edge
// Case 2). The handler additionally defends with its own enum check.
func reportInputSchema() (*jsonschema.Schema, error) {
	s, err := jsonschema.For[ReportArgs](nil)
	if err != nil {
		return nil, fmt.Errorf("inferring atcr_report schema: %w", err)
	}
	if p := s.Properties["format"]; p != nil {
		p.Enum = []any{report.FormatMarkdown, report.FormatJSON, report.FormatChecklist}
	}
	return s, nil
}
