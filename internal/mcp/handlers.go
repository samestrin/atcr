package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/report"
)

// engine is the shared state every handler closes over. Handlers are thin
// wrappers: they translate tool args into engine calls against the same
// internal packages the CLI uses, and translate results back. No business logic
// lives here (AC 04-03/04-04). root is the repo/.atcr location ("." in serve
// mode, a temp dir in tests); completer drives the fan-out; log writes to
// stderr in serve mode. bg tracks in-flight background reviews so the server can
// drain them on shutdown rather than orphaning a review mid-write.
type engine struct {
	root      string
	completer fanout.Completer
	log       *slog.Logger
	bg        sync.WaitGroup
}

// drain waits up to timeout for in-flight background reviews to finish, so a
// near-complete run is not abandoned mid-write when the client disconnects. A
// review still running past timeout is left to the process exit.
func (e *engine) drain(timeout time.Duration) {
	done := make(chan struct{})
	go func() { e.bg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// handleRange resolves a git range and returns its base/head plus commit and
// file counts. An empty diff is NOT an error here (unlike the CLI): it returns
// zero counts so a client can pre-flight "nothing to review" without exception
// handling (AC 04-04 Edge Case 5).
func (e *engine) handleRange(ctx context.Context, _ *mcpsdk.CallToolRequest, in RangeArgs) (*mcpsdk.CallToolResult, RangeResult, error) {
	if err := validateRangeArgs(in.Base, in.Head, in.MergeCommit); err != nil {
		return nil, RangeResult{}, err
	}
	res, err := gitrange.Resolve(ctx, e.root, gitrange.Options{Base: in.Base, Head: in.Head, MergeCommit: in.MergeCommit})
	if err != nil {
		if errors.Is(err, gitrange.ErrEmptyRange) {
			return nil, RangeResult{Base: in.Base, Head: in.Head, CommitCount: 0, FileCount: 0}, nil
		}
		return nil, RangeResult{}, rangeError(err)
	}
	fileCount, err := changedFileCount(ctx, e.root, res.Base, res.Head)
	if err != nil {
		return nil, RangeResult{}, fmt.Errorf("failed to resolve range: %w", err)
	}
	return nil, RangeResult{
		Base:          res.Base,
		Head:          res.Head,
		CommitCount:   res.CommitCount,
		FileCount:     fileCount,
		DetectionMode: res.DetectionMode,
		DefaultBranch: res.DefaultBranch,
		Shallow:       res.Shallow,
	}, nil
}

// handleReview resolves the range, loads config, scaffolds the review directory,
// and starts the fan-out in the background — returning immediately with the
// review id/path/agent-count and status "running". The fan-out continues in the
// server process (context detached from the request so it is not cancelled when
// the handler returns); clients poll atcr_status for completion (AC 04-03).
func (e *engine) handleReview(ctx context.Context, _ *mcpsdk.CallToolRequest, in ReviewArgs) (*mcpsdk.CallToolResult, ReviewResult, error) {
	if err := validateRangeArgs(in.Base, in.Head, in.MergeCommit); err != nil {
		return nil, ReviewResult{}, err
	}
	res, err := gitrange.Resolve(ctx, e.root, gitrange.Options{Base: in.Base, Head: in.Head, MergeCommit: in.MergeCommit})
	if err != nil {
		if errors.Is(err, gitrange.ErrNotARepository) {
			return nil, ReviewResult{}, fmt.Errorf("not a git repository: run atcr init first")
		}
		// An empty range is an error for review (unlike the atcr_range pre-flight):
		// scaffolding a review and firing the pool at an empty payload would waste
		// provider calls and repoint .atcr/latest at a no-op run.
		if errors.Is(err, gitrange.ErrEmptyRange) {
			return nil, ReviewResult{}, fmt.Errorf("nothing to review: %w", err)
		}
		return nil, ReviewResult{}, fmt.Errorf("resolve range: %w", err)
	}

	cfg, err := fanout.LoadReviewConfig(e.root, registry.CLIOverrides{})
	if err != nil {
		return nil, ReviewResult{}, err
	}

	now := time.Now()
	prep, err := fanout.PrepareReview(ctx, cfg, fanout.ReviewRequest{
		Repo: e.root,
		Root: e.root,
		Range: fanout.ReviewRange{
			Base:          res.Base,
			Head:          res.Head,
			DetectionMode: res.DetectionMode,
			DefaultBranch: res.DefaultBranch,
			CommitCount:   res.CommitCount,
		},
		Branch:     gitrange.CurrentBranch(ctx, e.root),
		Date:       now.Format("2006-01-02"),
		TimeSuffix: now.Format("150405"),
		StartedAt:  now,
		IDOverride: in.ID,
	})
	if err != nil {
		return nil, ReviewResult{}, err
	}

	// Run the fan-out after the handler returns. Detach from the request context
	// (cancelled on return) so the run continues in the server process; the
	// per-agent/global timeout is still enforced inside ExecuteReview. Completion
	// and the partial flag are observed via atcr_status / manifest.json on disk.
	// The goroutine is tracked (bg) so the server can drain it on shutdown, and a
	// recover() guard ensures a panic in the fan-out never crashes the server
	// process and its protocol connection.
	e.bg.Add(1)
	go func() {
		defer e.bg.Done()
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("review fan-out panicked", "review_id", prep.ID, "panic", r)
			}
		}()
		if _, err := fanout.ExecuteReview(context.WithoutCancel(ctx), e.completer, prep); err != nil {
			e.log.Error("review fan-out finished with errors", "review_id", prep.ID, "error", err)
		}
	}()

	return nil, ReviewResult{
		ReviewID:   prep.ID,
		ReviewPath: prep.Dir,
		Status:     runningStatus,
		AgentCount: prep.AgentCount(),
	}, nil
}

// handleReconcile merges a review's sources into reconciled artifacts and gates
// on fail_on. fail_on is validated before any work (AC 04-03 Edge Case 5). A
// review with no agent results is an error, not an empty success (Edge Case 3).
func (e *engine) handleReconcile(_ context.Context, _ *mcpsdk.CallToolRequest, in ReconcileArgs) (*mcpsdk.CallToolResult, ReconcileResult, error) {
	threshold := ""
	if in.FailOn != "" {
		t, err := reconcile.ParseSeverity(in.FailOn)
		if err != nil {
			return nil, ReconcileResult{}, err
		}
		threshold = t
	}

	dir, id, err := e.resolveReviewDir(in.IDOrPath)
	if err != nil {
		return nil, ReconcileResult{}, err
	}

	res, err := reconcile.RunReconcile(dir, nil, reconcile.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(dir),
	})
	if err != nil {
		return nil, ReconcileResult{}, err
	}
	if len(res.Summary.SourcesScanned) == 0 {
		return nil, ReconcileResult{}, fmt.Errorf("no agent results found in review %s; run atcr_review first", id)
	}

	out := ReconcileResult{
		ReviewID:      id,
		Pass:          true,
		TotalFindings: res.Summary.TotalFindings,
		Partial:       res.Summary.Partial,
		FailOn:        threshold,
	}
	if threshold != "" && reconcile.CountAtOrAbove(res.Findings, threshold) > 0 {
		out.Pass = false
		out.Findings = failingFindings(res, threshold)
	}
	return nil, out, nil
}

// handleReport renders a view over a review's reconciled findings. The format is
// validated both by the JSON Schema enum (before dispatch) and here as defense
// in depth for programmatic/in-process callers (AC 04-04 Edge Case 2).
func (e *engine) handleReport(_ context.Context, _ *mcpsdk.CallToolRequest, in ReportArgs) (*mcpsdk.CallToolResult, ReportResult, error) {
	format := in.Format
	if format == "" {
		format = report.FormatMarkdown
	}
	if !report.ValidFormat(format) {
		return nil, ReportResult{}, fmt.Errorf("invalid format: %s; must be one of: %s", format, report.Formats())
	}

	dir, id, err := e.resolveReviewDir(in.IDOrPath)
	if err != nil {
		return nil, ReportResult{}, err
	}

	findings, err := readReconciledFindings(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ReportResult{}, fmt.Errorf("review %s has no reconciliation results; run atcr_reconcile first", id)
		}
		return nil, ReportResult{}, err
	}

	var buf bytes.Buffer
	if err := report.Render(&buf, findings, format); err != nil {
		return nil, ReportResult{}, err
	}
	return nil, ReportResult{Format: format, Content: buf.String()}, nil
}

// handleStatus reports a review's fan-out progress, read from manifest.json and
// the pool summary.json. A missing review is "not found"; a corrupt manifest is
// a structured error, never a guessed result (AC 04-04 Edge Case 6).
func (e *engine) handleStatus(_ context.Context, _ *mcpsdk.CallToolRequest, in StatusArgs) (*mcpsdk.CallToolResult, StatusResult, error) {
	dir, id, err := e.resolveReviewDir(in.IDOrPath)
	if err != nil {
		return nil, StatusResult{}, err
	}
	st, err := fanout.ReadReviewStatus(dir, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, StatusResult{}, fmt.Errorf("review not found: %s", id)
		}
		return nil, StatusResult{}, err
	}
	return nil, *st, nil
}

// resolveReviewDir resolves the id_or_path anchor to an on-disk review directory
// and its id. An empty anchor falls back to .atcr/latest. A non-empty anchor is
// treated strictly as a review id and validated so a path separator, "..", an
// absolute path, or a leading dash can never escape .atcr/reviews/ — the MCP
// path-containment invariant (AC 04-03/04-04 Security). A missing latest pointer
// is the "no reviews found" error.
func (e *engine) resolveReviewDir(idOrPath string) (dir, id string, err error) {
	id = idOrPath
	if id == "" {
		latest, lerr := fanout.ReadLatest(e.root)
		if lerr != nil {
			return "", "", fmt.Errorf("no reviews found; run atcr_review first")
		}
		id = latest
	} else if verr := fanout.ValidateReviewID(id); verr != nil {
		return "", "", fmt.Errorf("invalid review id %q: %w", id, verr)
	}
	return filepath.Join(fanout.ReviewsRoot(e.root), id), id, nil
}

// validateRangeArgs enforces the argument combinations gitrange.Options leaves
// to its callers: the CLI guarantees them in validateRangeFlags, and the MCP
// handlers are a second caller that must do the same. Without this, merge_commit
// alongside base+head is silently ignored (the explicit branch wins) and
// merge_commit+head surfaces an error naming CLI flags that do not exist in the
// MCP arg vocabulary. The error is phrased in json field names (AC 04-04).
func validateRangeArgs(base, head, mergeCommit string) error {
	if mergeCommit != "" && (base != "" || head != "") {
		return fmt.Errorf("merge_commit cannot be combined with base or head")
	}
	if (base == "") != (head == "") {
		return fmt.Errorf("base and head must be provided together")
	}
	return nil
}

// changedFileCount counts the changed files in base..head by building the diff
// entries (one per file) via the same payload package the fan-out uses.
func changedFileCount(ctx context.Context, root, base, head string) (int, error) {
	entries, err := payload.BuildEntries(ctx, payload.ModeDiff, root, base, head)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// rangeError maps a resolution failure to a client-facing error, surfacing the
// not-a-repository case with the AC-mandated message (AC 04-04 Edge Case 3).
func rangeError(err error) error {
	if errors.Is(err, gitrange.ErrNotARepository) {
		return fmt.Errorf("not a git repository")
	}
	return fmt.Errorf("failed to resolve range: %w", err)
}

// failingFindings returns the reconciled findings at or above threshold as JSON
// records, so a failing gate can render them inline (AC 04-03 Scenario 7).
func failingFindings(res reconcile.Result, threshold string) []reconcile.JSONFinding {
	all := res.JSONFindings()
	out := make([]reconcile.JSONFinding, 0, len(all))
	for _, f := range all {
		if reconcile.AtOrAbove(f.Severity, threshold) {
			out = append(out, f)
		}
	}
	return out
}

// readReconciledFindings loads reviewDir/reconciled/findings.json. A missing
// file is returned as os.ErrNotExist so the caller can surface the "run
// atcr_reconcile first" guidance; malformed JSON surfaces a parse error.
func readReconciledFindings(reviewDir string) ([]reconcile.JSONFinding, error) {
	path := filepath.Join(reviewDir, "reconciled", "findings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err // includes os.ErrNotExist
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("reconciled findings.json is empty")
	}
	var findings []reconcile.JSONFinding
	if err := json.Unmarshal(data, &findings); err != nil {
		return nil, fmt.Errorf("parsing reconciled findings: %w", err)
	}
	return findings, nil
}
