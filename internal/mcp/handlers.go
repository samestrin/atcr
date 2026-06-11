package mcp

import (
	"bytes"
	"context"
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
func (e *engine) handleReconcile(ctx context.Context, _ *mcpsdk.CallToolRequest, in ReconcileArgs) (*mcpsdk.CallToolResult, ReconcileResult, error) {
	// Gate precedence parity with the CLI: explicit fail_on argument > project
	// config > user-global registry (no embedded default). Resolved and
	// validated before any work (AC 04-03 Edge Case 5).
	threshold := ""
	if raw, err := registry.ResolveGateThreshold(e.root, in.FailOn); err != nil {
		return nil, ReconcileResult{}, err
	} else if raw != "" {
		t, err := reconcile.ParseSeverity(raw)
		if err != nil {
			return nil, ReconcileResult{}, err
		}
		threshold = t
	}

	dir, id, err := e.resolveReviewDir(in.IDOrPath)
	if err != nil {
		return nil, ReconcileResult{}, err
	}

	// A running fan-out is rejected before any reconcile work: reading a
	// partially-written agent set would emit complete-looking artifacts and a
	// pass verdict computed from a subset of agents.
	if err := fanout.EnsureReviewComplete(dir, id); err != nil {
		return nil, ReconcileResult{}, err
	}

	// Fail-before-emit (mirrors the CLI's resolveReviewDir pre-check): a review
	// with no findings sources is rejected before RunReconcile so empty
	// reconciled artifacts are never written to disk as a side effect.
	sources, err := reconcile.Discover(filepath.Join(dir, "sources"), nil)
	if err != nil {
		return nil, ReconcileResult{}, err
	}
	if len(sources) == 0 {
		return nil, ReconcileResult{}, fmt.Errorf("no agent results found in review %s; run atcr_review first", id)
	}

	res, err := reconcile.RunReconcile(ctx, dir, nil, reconcile.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(dir),
	})
	if err != nil {
		return nil, ReconcileResult{}, err
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
func (e *engine) handleReport(ctx context.Context, _ *mcpsdk.CallToolRequest, in ReportArgs) (*mcpsdk.CallToolResult, ReportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, ReportResult{}, err
	}
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

	findings, err := reconcile.ReadReconciledFindings(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Distinguish "fan-out still running" from "reconcile not run yet" so
			// the guidance does not send a client to atcr_reconcile mid-review.
			if gerr := fanout.EnsureReviewComplete(dir, id); gerr != nil {
				return nil, ReportResult{}, gerr
			}
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
func (e *engine) handleStatus(ctx context.Context, _ *mcpsdk.CallToolRequest, in StatusArgs) (*mcpsdk.CallToolResult, StatusResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, StatusResult{}, err
	}
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
// path-containment invariant (AC 04-03/04-04 Security). A missing or unusable
// latest pointer is the "no reviews found" error, wrapping ReadLatest's cause
// so a corrupt/tampered pointer is not misreported as absent.
func (e *engine) resolveReviewDir(idOrPath string) (dir, id string, err error) {
	id = idOrPath
	if id == "" {
		latest, lerr := fanout.ReadLatest(e.root)
		if lerr != nil {
			// Wrap rather than discard: ReadLatest distinguishes a missing file
			// from a corrupt/tampered pointer, and that cause must surface.
			return "", "", fmt.Errorf("no reviews found; run atcr_review first: %w", lerr)
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
// invalid pairings surface errors naming CLI flags that do not exist in the MCP
// arg vocabulary. The rules mirror validateRangeFlags: base-only is valid (head
// defaults to HEAD, the natural CI-gate invocation); head requires base. Errors
// are phrased in json field names (AC 04-04).
func validateRangeArgs(base, head, mergeCommit string) error {
	if mergeCommit != "" && (base != "" || head != "") {
		return fmt.Errorf("merge_commit cannot be combined with base or head")
	}
	if head != "" && base == "" {
		return fmt.Errorf("head requires base")
	}
	return nil
}

// changedFileCount counts the changed files in base..head via the payload
// package's single name-status call — atcr_range is a cheap pre-flight, so it
// must not materialize per-file diff bodies just to count them.
func changedFileCount(ctx context.Context, root, base, head string) (int, error) {
	return payload.ChangedFileCount(ctx, root, base, head)
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


