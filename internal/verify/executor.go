package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// executorCompleter is the single-shot model call the fix-generation phase needs.
// *llmclient.Client satisfies it. It is the seam that lets tests drive fix
// generation with a scripted completer instead of a real provider.
type executorCompleter interface {
	Complete(ctx context.Context, inv llmclient.Invocation) (string, error)
}

// newExecutorClient builds the production executor completer. It is a package-level
// seam (rather than a runVerify parameter) so the executor wiring does not churn
// runVerify's many call sites; tests override it via swapExecutorClient.
var newExecutorClient = func() executorCompleter { return llmclient.New() }

// fixSnippetRadius is the number of lines read on each side of a finding's line to
// give the executor real code context (Epic 7.0 snippet tier).
const fixSnippetRadius = 30

// fixAttributionPrefix is the marker appended to a finding's Evidence after a fix
// is generated; it doubles as the idempotency guard (a finding already carrying it
// is not re-generated on a verify re-run).
const fixAttributionPrefix = "fix by "

// anyFixEligible reports whether at least one finding qualifies for fix generation
// on the executor's confidence+severity gate (the same per-finding gate
// generateFixes applies). The pipeline uses it to avoid building BOTH the snapshot
// harness and the executor client for a registry whose findings yield zero fixes.
func anyFixEligible(findings []reconcile.JSONFinding, ex *registry.ExecutorConfig) bool {
	fixMinSev := ex.EffectiveFixMinSeverity()
	for i := range findings {
		if reconcile.ConfidenceAtOrAbove(findings[i].Confidence, reconcile.ConfHigh) && meetsSeverityFloor(findings[i].Severity, fixMinSev) {
			return true
		}
	}
	return false
}

// generateFixes is the fix-generation phase (Epic 7.0). For every finding whose
// confidence is HIGH-or-better (so VERIFIED — the tier the verify stage promotes
// confirmed findings to — is included) AND whose severity meets the executor's
// min_severity_for_fix floor, it reads a code snippet around the finding from the
// review snapshot (via the read-only dispatcher) and asks the single executor model
// for a minimal fix, writing it into the finding's Fix column and appending
// "fix by <name>" to Evidence.
//
// It mutates findings in place and is run after verdict application / confidence
// recompute and before the artifacts are serialized, so fixes ride into
// findings.json. Eligible findings are processed by a bounded worker pool (cap
// reg.Verify.MaxParallel, default 4) rather than serially — each fix is an
// independent executor round-trip — and every worker mutates only its own
// findings element, the same per-index-write invariant the skeptic stage relies
// on, so no mutex is needed. Failure isolation mirrors the verify stage: a snippet read
// failure, an executor error, or an empty completion leaves that finding's existing
// Fix/Evidence untouched and is logged, never returned — fix generation never fails
// the run. A nil executor or completer is a no-op; disp may be nil (snapshot
// unavailable), in which case the snippet is omitted and the executor works from the
// finding text alone.
func generateFixes(ctx context.Context, findings []reconcile.JSONFinding, ex *registry.ExecutorConfig, reg *registry.Registry, complete executorCompleter, disp Dispatcher, sharedTimeoutSecs int) {
	if ex == nil || complete == nil {
		return
	}
	// Defense-in-depth: registry validation already guarantees the executor's
	// provider is defined, so this guard never fires on a validated registry. It
	// protects the direct-call path (e.g. tests building a Registry in memory) from
	// a nil-map panic and documents the invariant rather than assuming it.
	prov, ok := reg.Providers[ex.Provider]
	if !ok {
		logPipelineWarning(log.FromContext(ctx), "executor_unknown_provider", ex.Provider)
		return
	}
	minSev := ex.EffectiveFixMinSeverity()
	// Bounded worker pool (mirrors the skeptic stage in pipeline.go): the
	// eligibility filters below are cheap and stay on the calling goroutine, but
	// each eligible finding's snippet read + executor round-trip + writes run in
	// their own goroutine under a semaphore capped at reg.Verify.MaxParallel.
	maxPar := reg.Verify.MaxParallel
	if maxPar <= 0 {
		maxPar = 4
	}
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	for i := range findings {
		// Bail promptly on cancellation: without this the loop keeps enqueuing
		// every remaining finding even after ctx is done, and a nil fix_timeout
		// can leave each callExecutor blocked on a provider that ignores ctx.
		if ctx.Err() != nil {
			break
		}
		f := &findings[i]
		if !reconcile.ConfidenceAtOrAbove(f.Confidence, reconcile.ConfHigh) {
			continue
		}
		if !meetsSeverityFloor(f.Severity, minSev) {
			continue
		}
		// Idempotency + cost control: a finding this executor already attributed (a
		// prior verify run generated its fix) is not re-generated. The guard matches
		// a delimited "; "-token, not a raw substring, so a name that is a strict
		// prefix of another ("op" vs "opus") or unrelated evidence prose containing
		// "fix by <name>" mid-sentence does not silently suppress generation.
		if hasFixAttribution(f.Evidence, ex.Name) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f *reconcile.JSONFinding) {
			defer wg.Done()
			defer func() { <-sem }()
			snippet := readFixSnippet(ctx, disp, f.File, f.Line)
			prompt := buildFixPrompt(*f, snippet, ex.Persona)
			out, err := callExecutor(ctx, complete, prov, ex, prompt, sharedTimeoutSecs)
			if err != nil {
				logPipelineWarning(log.FromContext(ctx), "executor_fix_failed", fmt.Sprintf("%s:%d: %v", f.File, f.Line, err))
				f.FixWarning = "fix generation failed: " + err.Error()
				return
			}
			fix := strings.TrimSpace(out)
			if fix == "" {
				logPipelineWarning(log.FromContext(ctx), "executor_empty_fix", fmt.Sprintf("%s:%d", f.File, f.Line))
				f.FixWarning = "fix generation returned an empty completion"
				return
			}
			f.Fix = fix
			// Clear any warning a prior failed/empty run left on this finding so it
			// never carries both a valid Fix and a stale "fix is absent" warning.
			f.FixWarning = ""
			f.Evidence = appendFixAttribution(f.Evidence, ex.Name)
		}(f)
	}
	wg.Wait()
}

// callExecutor invokes the executor model for one finding, applying a per-call
// deadline scoped to this single call. The deadline is the executor's own
// fix_timeout when set, otherwise the resolved shared verify timeout (600s
// default) — see ExecutorConfig.EffectiveExecutorTimeoutSecs. It is applied
// unconditionally so a default executor (nil fix_timeout) against a hung provider
// cannot block the verify run unbounded.
func callExecutor(ctx context.Context, complete executorCompleter, prov registry.Provider, ex *registry.ExecutorConfig, prompt string, sharedTimeoutSecs int) (string, error) {
	timeout := ex.EffectiveExecutorTimeoutSecs(registry.Settings{TimeoutSecs: sharedTimeoutSecs})
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	// Temperature is sent on every executor call (Epic 7.0.1): the resolver supplies
	// the deterministic 0.0 default when temperature is unset, so fixes are
	// predictable rather than inheriting the provider's own (often higher) default.
	// Bind to a local so its address outlives this expression.
	temp := ex.EffectiveExecutorTemperature()
	return complete.Complete(callCtx, llmclient.Invocation{
		BaseURL:     prov.BaseURL,
		APIKeyEnv:   prov.APIKeyEnv,
		Model:       ex.Model,
		Temperature: &temp,
		Prompt:      prompt,
	})
}

// readFixSnippet reads up to fixSnippetRadius lines on each side of line from file
// in the review snapshot, via the read-only read_file tool on the dispatcher. It
// returns "" (best-effort) when the dispatcher is unavailable, the file is empty,
// or the read fails — the executor then works from the finding text alone.
func readFixSnippet(ctx context.Context, disp Dispatcher, file string, line int) string {
	if disp == nil {
		return ""
	}
	start := line - fixSnippetRadius
	if start < 1 {
		start = 1
	}
	end := line + fixSnippetRadius
	args, err := json.Marshal(struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}{Path: file, StartLine: start, EndLine: end})
	if err != nil {
		logPipelineWarning(log.FromContext(ctx), "fix_snippet_unavailable", fmt.Sprintf("%s:%d: %v", file, line, err))
		return ""
	}
	res, err := disp.Execute(ctx, "read_file", args)
	if err != nil {
		logPipelineWarning(log.FromContext(ctx), "fix_snippet_unavailable", fmt.Sprintf("%s:%d: %v", file, line, err))
		return ""
	}
	return res.Content
}

// buildFixPrompt renders the executor prompt for one finding: a fix-focused persona
// instruction, the finding metadata, the reviewer's existing fix suggestion (when
// present, to refine rather than reinvent), and the source snippet (when available).
//
// Untrusted-data boundary: persona, the finding fields (Problem, Fix), and the
// source snippet are all interpolated verbatim into this single prompt string —
// llmclient.Invocation carries no role separation, so there is no structured way to
// fence them. The persona is sanitized at load (validateExecutor rejects control
// characters and caps length) to block CR/LF prompt-line forgery; the finding text
// and snippet are reviewer/repo-derived data, not instructions. Blast radius is
// bounded: the registry is self-authored and the output only lands in the Fix
// column (it is never executed), so this is documented rather than actively escaped.
func buildFixPrompt(f reconcile.JSONFinding, snippet, persona string) string {
	if strings.TrimSpace(persona) == "" {
		persona = registry.DefaultExecutorPersona
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a code-fix executor. Generate the minimal code change that fixes the finding below. Preserve the existing style and conventions. Output only the fix (corrected code or a precise change instruction); do not restate the problem.\n\n", persona)
	fmt.Fprintf(&b, "Severity: %s\nLocation: %s:%d\nCategory: %s\nProblem: %s\n", f.Severity, f.File, f.Line, f.Category, f.Problem)
	if strings.TrimSpace(f.Fix) != "" {
		fmt.Fprintf(&b, "Reviewer-suggested fix (refine into a minimal, correct change): %s\n", f.Fix)
	}
	if strings.TrimSpace(snippet) != "" {
		fmt.Fprintf(&b, "\nSource context around %s:%d (each line is prefixed with its line number as `N: `; do NOT include those line-number prefixes in your fix):\n```\n%s\n```\n", f.File, f.Line, snippet)
	}
	return b.String()
}

// hasFixAttribution reports whether evidence already carries this executor's
// "fix by <name>" attribution as a delimited "; "-separated token. Matching a
// whole token rather than a raw substring prevents a name that is a strict prefix
// of another ("op" vs "opus") — or unrelated evidence prose containing
// "fix by <name>" mid-sentence — from being falsely treated as already-attributed.
func hasFixAttribution(evidence, name string) bool {
	attr := fixAttributionPrefix + name
	for _, seg := range strings.Split(evidence, "; ") {
		if strings.TrimSpace(seg) == attr {
			return true
		}
	}
	return false
}

// appendFixAttribution appends "fix by <name>" to a finding's Evidence, joining
// with the existing separator. It is idempotent: an Evidence already carrying the
// attribution as a delimited token is returned unchanged.
func appendFixAttribution(evidence, name string) string {
	attr := fixAttributionPrefix + name
	if strings.TrimSpace(evidence) == "" {
		return attr
	}
	if hasFixAttribution(evidence, name) {
		return evidence
	}
	return evidence + "; " + attr
}
