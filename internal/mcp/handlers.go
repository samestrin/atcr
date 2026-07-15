package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/atcr/internal/debate"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/report"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/internal/verify"
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
	diag      io.Writer
	bg        sync.WaitGroup
	// shutdownCtx is cancelled (via shutdownCancel) when the server begins
	// shutdown — Serve fires it after the transport loop returns, before drain.
	// Detached background reviews tie their cancellation to it (withShutdownCancel)
	// so a server shutdown cuts them off like the CLI's SIGINT path and they record
	// Interrupted=true, while a handler returning normally leaves them untouched
	// (epic 4.1.2). nil when the engine is built outside buildServer (NewServer
	// discards its engine; tests may construct &engine{} directly).
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

// diagWriter resolves the engine's scorecard-diagnostics sink: the injected
// writer, or os.Stderr when unset. It mirrors scorecard's own default-to-stderr
// rule at the MCP construction site so a nil diag never reaches EmitOpts as a
// surprise (Epic 3.4 AC4).
//
// This guard intentionally covers only the plain e.diag==nil case. The typed-nil
// guard — a non-nil io.Writer wrapping a nil pointer, e.g. (*bytes.Buffer)(nil) —
// is delegated to scorecard.diagWriter, which re-applies its isNilPointer check
// downstream before any write. The typed-nil normalization lives in one place
// (the scorecard package that owns the diagnostics contract) rather than being
// duplicated at every call site.
func (e *engine) diagWriter() io.Writer {
	if e.diag == nil {
		return os.Stderr
	}
	return e.diag
}

// logger returns the engine's logger, or a no-op discard logger when log is
// nil. Mirrors the nil-logger guard in buildServer so direct engine{} test
// construction is safe even when the log field is omitted.
func (e *engine) logger() *slog.Logger {
	if e.log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return e.log
}

// reviewContext detaches ctx for the background fan-out (so the run is not
// cancelled when the handler returns) and seeds it with the server logger tagged
// by review_id, so every fan-out log line for this review is greppable by
// review_id (AC9). This mirrors the CLI review path (cmd/atcr/review.go
// correlateReviewID) for the MCP entry point; Phase 4 fan-out reads the logger
// back via log.FromContext.
//
// secrets are the resolved registry API key values (PreparedReview.SecretValues);
// they are passed by value into NewRedactor so the exact-value scrub is live in
// serve mode (epic 4.9) — non-sk-/non-Bearer keys are scrubbed by value, not only
// by token shape — and are never logged.
func (e *engine) reviewContext(ctx context.Context, reviewID string, secrets ...string) context.Context {
	// Seed review_id and enforce sink-level redaction (configured secret values +
	// secret-shaped tokens → AC5, absolute paths under the repo root → AC6) so the
	// serve-mode fan-out matches the CLI path's single-sink redaction contract
	// (TD-007). Resolve the root to absolute first — e.root is "." in serve mode and
	// relativizePaths no-ops on ".", so AC6 needs the concrete root.
	root := e.root
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	logger := log.WithRedactor(log.WithReviewID(e.logger(), reviewID), log.NewRedactor(root, secrets...))
	return log.NewContext(context.WithoutCancel(ctx), logger)
}

// withShutdownCancel derives a child of ctx that is cancelled when the server
// begins shutdown — and ONLY then. The server-lifecycle signal (e.shutdownCtx,
// fired by Serve after the transport loop returns) is the sole cancellation
// source; the request handler returning never reaches it, because the ctx passed
// here is already detached via reviewContext's context.WithoutCancel. So a
// detached review stays detached across a normal handler return (AC2) but is cut
// off like the CLI's SIGINT path on server shutdown (AC1), letting ExecuteReview's
// existing ctx.Err()==Canceled marker record Interrupted=true with no out-of-band
// manifest write (no race with the fan-out's own write).
//
// The returned cancel MUST be deferred by the caller for the lifetime of the
// review: it stops the AfterFunc registration so a completed review does not
// retain its cancelCtx on e.shutdownCtx until process exit. When e.shutdownCtx is
// nil (engine built outside buildServer), it is a no-op passthrough.
//
// If shutdown already fired, AfterFunc has concurrently invoked cancel and stop()
// returns false; the returned closure then calls cancel() a second time — a
// deliberate idempotent no-op, as context.CancelFunc is safe to call repeatedly.
func (e *engine) withShutdownCancel(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.shutdownCtx == nil {
		return ctx, func() {}
	}
	cctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.shutdownCtx, cancel)
	return cctx, func() { stop(); cancel() }
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
		// The explicit-id collision message names CLI-only flags (--resume/--force);
		// re-message it for MCP clients, which have neither.
		var existsErr *fanout.ReviewDirExistsError
		if errors.As(err, &existsErr) {
			return nil, ReviewResult{}, fmt.Errorf("review %s already exists; choose a different id or remove the existing review to re-run", existsErr.ID)
		}
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
				e.logger().Error("review fan-out panicked", "review_id", prep.ID, "panic", r)
			}
		}()
		// Detach for handler-return (reviewContext: WithoutCancel), then re-attach a
		// cancellation tied to server shutdown only. cancel is deferred so a review
		// that finishes before any shutdown releases its AfterFunc registration.
		secrets, secretWarnings := prep.SecretValues()
		for _, w := range secretWarnings {
			e.logger().Debug(w, "review_id", prep.ID)
		}
		rctx, cancel := e.withShutdownCancel(e.reviewContext(ctx, prep.ID, secrets...))
		defer cancel()
		if _, err := fanout.ExecuteReview(rctx, e.completer, prep); err != nil {
			e.logger().Error("review fan-out finished with errors", "review_id", prep.ID, "error", err)
		}
		// Observability parity with the CLI's "review interrupted by signal"
		// (epic 4.1/4.1.1): when server shutdown cut this detached review off,
		// emit a greppable structured Warn so monitoring/CI finds interrupted
		// serve-mode reviews in logs, not only via the on-disk manifest.
		if e.shutdownCtx != nil && e.shutdownCtx.Err() != nil {
			e.logger().Warn("review interrupted by server shutdown", "review_id", prep.ID)
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

	// --require-verified is meaningless without a gate (the same fail-fast rule as
	// the CLI, AC 05-01 EC3): a strict gate that never runs gives false confidence.
	if in.RequireVerified && threshold == "" {
		return nil, ReconcileResult{}, fmt.Errorf("require_verified requires fail_on")
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

	res, err := reconcile.RunReconcile(ctx, dir, nil, reclib.Options{
		ReconciledAt: time.Now(),
		Partial:      fanout.ReadManifestPartial(dir),
		// Empty root: the MCP server operates on a review-artifact dir, not a
		// checked-out source tree, so it must NOT assume its cwd is the reviewed
		// repo. An empty root hard-disables AST file reads (proximity fallback) and
		// makes finding-path validation a no-op, rather than keying findings off
		// unrelated same-named files under the server's cwd (Epic 13.1 TD).
		Root: "",
	})
	if err != nil {
		return nil, ReconcileResult{}, err
	}

	// Emit the per-run scorecard (Epic 3.3) via the same shared bridge the CLI
	// reconcile uses, so MCP-driven and CLI-driven reconciles produce identical
	// scorecard records (TD-005 — no entry-point divergence). The MCP path has no
	// suppression flag, so it always emits. Scorecard diagnostics route to the
	// engine's diag sink — os.Stderr by default (the documented MCP default,
	// which has no cobra cmd to source a writer from), or an injected writer in
	// tests so the wiring is assertable (Epic 3.4 AC4). Best-effort.
	scorecard.EmitForReconcile(dir, res, scorecard.EmitOpts{Diag: e.diagWriter()})

	// TD-004: warn when verify never ran — the gate would trivially pass everything.
	if in.RequireVerified {
		if verr := reconcile.ValidateRequireVerified(dir); verr != nil {
			e.logger().Warn("require_verified: verify stage not complete", "detail", verr.Error())
		}
	}

	out := ReconcileResult{
		ReviewID:      id,
		Pass:          true,
		TotalFindings: res.Summary.TotalFindings,
		Partial:       res.Summary.Partial,
		FailOn:        threshold,
	}
	if threshold != "" && reconcile.CountAtOrAbove(res.Findings, threshold, in.RequireVerified) > 0 {
		out.Pass = false
		out.Findings = failingFindings(res, threshold, in.RequireVerified)
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
	if format == report.FormatMarkdown {
		// The markdown report carries the disagreement radar above its findings
		// (Epic 3.2). A corrupt ambiguous.json degrades to a findings-only radar
		// rather than failing the report.
		//
		// MCP/CLI parity: this embedded markdown radar is identical to the CLI's —
		// both call report.RenderMarkdownWithDisagreements (cmd/atcr/report.go). The
		// CLI's standalone --disagreements ranked view (RenderDisagreements /
		// RenderDisagreementsJSON) is intentionally NOT exposed over MCP; MCP clients
		// receive the radar markdown-embedded only. Any renderer-dedup work belongs
		// to Epic 15.0 (radar-renderer-consolidation), not this surface.
		df := reconcile.LoadDisagreements(dir, findings)
		if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
			return nil, ReportResult{}, err
		}
	} else if err := report.Render(&buf, findings, format); err != nil {
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

// handleMetrics renders the process-wide metrics registry in Prometheus text
// exposition format (Epic 4.4). The epic's AC4 named an HTTP /metrics endpoint,
// but atcr serve is a stdio JSON-RPC server with no HTTP listener, so metrics
// are surfaced as this tool instead (see the epic Clarifications). Metrics are
// cumulative since the server started.
//
// Security posture: this tool is unauthenticated by design. atcr serve binds
// only a stdio transport (server.go) — there is no network listener to reach it
// over — so process-local access IS the accepted control (epic Risk table +
// 2026-06-19 Clarification, which mark an HTTP /metrics listener out of scope).
// Token validation presupposes a networked transport that does not exist; add it
// only if such a transport is ever introduced.
func (e *engine) handleMetrics(ctx context.Context, _ *mcpsdk.CallToolRequest, _ MetricsArgs) (*mcpsdk.CallToolResult, MetricsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, MetricsResult{}, err
	}
	return nil, MetricsResult{Format: "prometheus", Content: metrics.DefaultRegistry.WritePrometheus()}, nil
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
// must not materialize per-file diff bodies just to count them. The returned
// count is ignore-filtered: it excludes files matched by repo-root .gitignore
// or .atcrignore rules, matching the default review payload.
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

// failingFindings returns the reconciled findings that fail the gate as JSON
// records, so a failing gate can render them inline (AC 04-03 Scenario 7). It
// applies the SAME reconcile.IsFailing predicate the count uses — refuted
// excluded, out-of-scope excluded, and under requireVerified only VERIFIED — so
// the inline list never diverges from the pass/fail verdict (AC 05-02). The JSON
// records preserve their 1:1 order with res.Findings, so the merged finding's
// verification block (if any) rides along.
func failingFindings(res reconcile.Result, threshold string, requireVerified bool) []reconcile.JSONFinding {
	jsons := res.JSONFindings()
	out := make([]reconcile.JSONFinding, 0, len(jsons))
	for i, m := range res.Findings {
		if reconcile.IsFailing(m.Severity, m.Category, m.Verification, threshold, requireVerified) {
			out = append(out, jsons[i])
		}
	}
	return out
}

// handleVerify runs adversarial verification over a review's reconciled findings
// and returns the verdict tally plus an optional gate status. It shares the
// internal/verify.Verify orchestration with the `atcr verify` CLI, so MCP and
// CLI emit identical artifacts for the same input (AC 04-03/04-04). failOn /
// requireVerified are validated before any work; missing reconciled findings
// yields the same reconcile-first guidance as the CLI. A skeptic failure becomes
// an unverifiable verdict, never a handler error (AC 04-03 Error Scenario 3).
func (e *engine) handleVerify(ctx context.Context, _ *mcpsdk.CallToolRequest, in VerifyArgs) (*mcpsdk.CallToolResult, VerifyResult, error) {
	minSev, err := parseOptionalSeverity(in.MinSeverity)
	if err != nil {
		return nil, VerifyResult{}, err
	}
	threshold, err := parseOptionalSeverity(in.FailOn)
	if err != nil {
		return nil, VerifyResult{}, err
	}
	if in.RequireVerified && threshold == "" {
		return nil, VerifyResult{}, fmt.Errorf("requireVerified requires failOn")
	}

	dir, id, err := e.resolveReviewDir(in.IDOrPath)
	if err != nil {
		return nil, VerifyResult{}, err
	}

	reg, err := e.loadVerifyRegistry(in.RegistryPath)
	if err != nil {
		return nil, VerifyResult{}, err
	}

	verifyRoot := e.root
	if abs, aerr := filepath.Abs(verifyRoot); aerr == nil {
		verifyRoot = abs
	}
	res, err := verify.Verify(ctx, e.root, dir, reg, verify.Options{
		Fresh:       in.Fresh,
		Thorough:    in.Thorough,
		MinSeverity: minSev,
		// This path loads only the registry (no resolved Settings), so leave the
		// shared timeout at 0; EffectiveExecutorTimeoutSecs falls back to the 600s
		// default, keeping the executor call bounded.
		SharedTimeoutSecs: 0,
		// Scrub configured registry secrets from reproduced exec evidence before it
		// is persisted into findings.json (Epic 11.0), mirroring reviewContext's
		// log-sink redactor for the data artifact the sink never sees.
		Redactor: log.NewRedactor(verifyRoot, fanout.RegistrySecretValues(reg)...),
	})
	if err != nil {
		if errors.Is(err, verify.ErrNoReconciledFindings) {
			return nil, VerifyResult{}, fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", dir)
		}
		return nil, VerifyResult{}, err
	}

	out := VerifyResult{
		ReviewID:          id,
		VerdictCounts:     res.VerdictCounts,
		FindingsProcessed: res.FindingsProcessed,
		DurationMs:        res.DurationMs,
	}
	if threshold != "" {
		findings, ferr := reconcile.ReadReconciledFindings(dir)
		if ferr != nil {
			return nil, VerifyResult{}, ferr
		}
		n := reconcile.CountFailingJSON(findings, threshold, in.RequireVerified)
		out.GateStatus = &GateStatus{Pass: n == 0, FailingCount: n, FailOn: threshold}
	}
	return nil, out, nil
}

// handleDebate runs the cross-examination stage over a review's reconciled
// findings and returns the per-outcome tally plus an optional gate status. It
// shares the internal/debate.Debate orchestration with the `atcr debate` CLI and
// the `atcr review --debate` chain, so all three emit identical artifacts for the
// same input. failOn / requireVerified are validated before any work; missing
// reconciled findings yields the same reconcile-first guidance as the CLI. A seat
// failure becomes an unresolved item, never a handler error.
func (e *engine) handleDebate(ctx context.Context, _ *mcpsdk.CallToolRequest, in DebateArgs) (*mcpsdk.CallToolResult, DebateResult, error) {
	threshold, err := parseOptionalSeverity(in.FailOn)
	if err != nil {
		return nil, DebateResult{}, err
	}
	if in.RequireVerified && threshold == "" {
		return nil, DebateResult{}, fmt.Errorf("requireVerified requires failOn")
	}

	dir, id, err := e.resolveReviewDir(in.IDOrPath)
	if err != nil {
		return nil, DebateResult{}, err
	}

	reg, err := e.loadVerifyRegistry(in.RegistryPath)
	if err != nil {
		return nil, DebateResult{}, err
	}

	res, err := debate.Debate(ctx, e.root, dir, reg, debate.Options{SingleModel: in.SingleModel})
	if err != nil {
		if errors.Is(err, debate.ErrNoReconciledFindings) {
			return nil, DebateResult{}, fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", dir)
		}
		return nil, DebateResult{}, err
	}

	out := DebateResult{
		ReviewID:   id,
		Selected:   res.Selected,
		Upheld:     res.Upheld,
		Overturned: res.Overturned,
		Split:      res.Split,
		Unresolved: res.Unresolved,
		Overflow:   res.Overflow,
		DurationMs: res.DurationMs,
	}
	if threshold != "" {
		findings, ferr := reconcile.ReadReconciledFindings(dir)
		if ferr != nil {
			return nil, DebateResult{}, ferr
		}
		n := reconcile.CountFailingJSON(findings, threshold, in.RequireVerified)
		out.GateStatus = &GateStatus{Pass: n == 0, FailingCount: n, FailOn: threshold}
	}
	return nil, out, nil
}

// loadVerifyRegistry resolves the registry the verify stage selects skeptics
// from: an explicit registryPath when supplied, else the user/project merged
// registry the other handlers use (so a default verify call sees the same
// roster as review/reconcile).
func (e *engine) loadVerifyRegistry(path string) (*registry.Registry, error) {
	if p := strings.TrimSpace(path); p != "" {
		// Absolute paths are rejected upfront because filepath.Join appends them
		// rather than replacing the base, so the canonical check below would
		// incorrectly allow "/etc/passwd" as root+"/etc/passwd".
		if filepath.IsAbs(p) {
			return nil, fmt.Errorf("invalid registryPath %q: must be a relative path within the project", path)
		}
		// Canonical containment: filepath.Clean resolves all .. segments so balanced
		// traversals like "a/.." (which don't contain "../" as a substring) are
		// caught regardless of how .. segments balance.
		cleanRoot := filepath.Clean(e.root)
		resolved := filepath.Clean(filepath.Join(e.root, p))
		if !strings.HasPrefix(resolved, cleanRoot+string(filepath.Separator)) {
			return nil, fmt.Errorf("invalid registryPath %q: must be a relative path within the project", path)
		}
		return registry.LoadRegistry(resolved)
	}
	cfg, err := fanout.LoadReviewConfig(e.root, registry.CLIOverrides{})
	if err != nil {
		return nil, err
	}
	return cfg.Registry, nil
}

// parseOptionalSeverity canonicalizes an optional severity arg: "" (or
// whitespace) means unset; any other value must be a valid severity or it is a
// handler error (the threshold-validation order is unaffected by other flags).
func parseOptionalSeverity(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	return reconcile.ParseSeverity(s)
}
